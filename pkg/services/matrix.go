package services

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"os"
	"path"
	"strings"
	"sync"

	_ "github.com/mattn/go-sqlite3"
	log "github.com/sirupsen/logrus"
	"maunium.net/go/maulogger/v2"
	"maunium.net/go/mautrix"
	"maunium.net/go/mautrix/crypto"
	"maunium.net/go/mautrix/event"
	"maunium.net/go/mautrix/format"
	"maunium.net/go/mautrix/id"
	"maunium.net/go/mautrix/util/dbutil"
)

type MatrixOptions struct {
	AccessToken   string      `json:"accessToken"`
	DeviceID      id.DeviceID `json:"deviceID"`
	HomeserverURL string      `json:"homeserverURL,omitempty"`
	UserID        id.UserID   `json:"userID"`

	DataPath string `json:"dataPath,omitempty"`
}

func NewMatrixService(opts MatrixOptions) (NotificationService, error) {
	homeserverURL := opts.HomeserverURL
	if homeserverURL == "" {
		_, serverName, err := opts.UserID.Parse()
		if err != nil {
			return nil, fmt.Errorf("couldn't parse user ID '%s' for server name: %w", opts.UserID, err)
		}
		resp, err := mautrix.DiscoverClientAPI(serverName)
		if err != nil {
			return nil, fmt.Errorf("couldn't discover client URL for homeserver '%s'; try setting matrix.homeserverURL: %w", serverName, err)
		}
		homeserverURL = resp.Homeserver.BaseURL
	}
	client, err := mautrix.NewClient(homeserverURL, opts.UserID, opts.AccessToken)
	if err != nil {
		return nil, fmt.Errorf("failed to create matrix client: %w", err)
	}
	// normally gets set during client.Login
	client.DeviceID = opts.DeviceID

	// set up e2ee if possible
	var olmMachine *crypto.OlmMachine
	if dataPath := opts.DataPath; dataPath != "" {
		olmMachine, err = matrixInitCrypto(dataPath, client)
		if err != nil {
			return nil, fmt.Errorf("couldn't initialize matrix crypto: %w", err)
		}
	} else {
		// when there's no dataPath, we can't store e2ee keys
		log.Infof("no dataPath configured for matrix service; skipping end-to-end encryption setup")
		syncer := client.Syncer.(*mautrix.DefaultSyncer)
		syncer.OnEvent(client.Store.(*mautrix.InMemoryStore).UpdateState)
	}

	go func() {
		err := client.Sync()
		if err != nil {
			log.Errorf("matrix client sync failed: %v", err)
		}
	}()

	return &matrixService{
		client,
		olmMachine,
	}, nil
}

type matrixService struct {
	client     *mautrix.Client
	olmMachine *crypto.OlmMachine
}

func (s *matrixService) Send(notification Notification, dest Destination) error {
	if len(dest.Recipient) == 0 {
		return fmt.Errorf("destination cannot be empty")
	}

	client := s.client
	cryptoDisabled := s.olmMachine == nil

	// assume destination is a room ID
	roomID := id.RoomID(dest.Recipient)
	serverName := ""

	// check if destination is instead a room alias
	if dest.Recipient[0] == '#' {
		// resolve room alias to room ID
		roomAlias := id.RoomAlias(dest.Recipient)
		resp, err := client.ResolveAlias(roomAlias)
		if err != nil {
			return fmt.Errorf("couldn't resolve room alias '%s': %w", dest.Recipient, err)
		}
		roomID = resp.RoomID
		_, serverName, _ = strings.Cut(roomAlias.String(), ":")
	}

	// join room if needed and possible
	room := client.Store.LoadRoom(roomID)
	mem := room.GetMembershipState(client.UserID)
	isEncrypted := room.GetStateEvent(event.StateEncryption, "") != nil
	if mem == event.MembershipBan {
		return fmt.Errorf("can't send to matrix room '%s' where we're banned", roomID)
	} else if mem != event.MembershipJoin {
		if isEncrypted && cryptoDisabled {
			return fmt.Errorf("won't join encrypted matrix room '%s' when crypto is not configured; make sure dataPath is set", roomID)
		}
		_, err := client.JoinRoom(roomID.String(), serverName, nil)
		if err != nil {
			return fmt.Errorf("couldn't join matrix room '%s': %w", roomID, err)
		}
	}

	// message content
	markdownContent := format.RenderMarkdown(notification.Message, true, true)
	evtType := event.EventMessage
	var content interface{} = &event.MessageEventContent{
		MsgType: event.MsgNotice,
		Body:    markdownContent.Body,

		Format:        markdownContent.Format,
		FormattedBody: markdownContent.FormattedBody,
	}

	// encrypt event when we need to and error if e2ee isn't setup
	if isEncrypted {
		if cryptoDisabled {
			return fmt.Errorf("can't send to encrypted matrix room since crypto is not setup; make sure dataPath is set")
		}
		store := client.Store.(*matrixStore)
		triedSharingGroupSession := false
	encrypt:
		encrypted, err := s.olmMachine.EncryptMegolmEvent(roomID, evtType, content)
		if err != nil {
			if isBadEncryptError(err) {
				return fmt.Errorf("couldn't encrypt matrix event: %w", err)
			}
			if triedSharingGroupSession {
				return fmt.Errorf("couldn't encrypt matrix event even after sharing group session: %w", err)
			}

			log.Debugf("got '%v' error while trying to encrypt matrix message; sharing group session and trying again...", err)
			err = s.olmMachine.ShareGroupSession(roomID, store.GetRoomMembers(roomID))
			if err != nil {
				return fmt.Errorf("couldn't share matrix group session: %w", err)
			}
			triedSharingGroupSession = true
			goto encrypt
		}
		evtType = event.EventEncrypted
		content = encrypted
	}

	// send the event
	r, err := client.SendMessageEvent(roomID, evtType, content)
	if err != nil {
		return fmt.Errorf("couldn't send matrix message: %w", err)
	}
	log.Infof("sent matrix event '%s'", r.EventID.String())

	return nil
}

func isBadEncryptError(err error) bool {
	return err != crypto.SessionExpired && err != crypto.SessionNotShared && err != crypto.NoGroupSession
}

func matrixInitCrypto(dataPath string, client *mautrix.Client) (*crypto.OlmMachine, error) {
	store, err := newMatrixStore(dataPath)
	if err != nil {
		return nil, fmt.Errorf("couldn't create matrix store for crypto: %w", err)
	}

	client.Syncer = newMatrixSyncer(store)
	client.Store = store

	cryptoLogger := matrixCryptoLogger{}

	cryptoDB, err := sql.Open("sqlite3", path.Join(dataPath, "crypto.db"))
	if err != nil {
		return nil, fmt.Errorf("couldn't open crypto db: %w", err)
	}

	db, err := dbutil.NewWithDB(cryptoDB, "sqlite3")
	if err != nil {
		return nil, fmt.Errorf("couldn't create crypto db: %w", err)
	}

	cryptoStore := crypto.NewSQLCryptoStore(
		db,
		dbutil.MauLogger(maulogger.DefaultLogger),
		fmt.Sprintf("%s/%s", client.UserID, client.DeviceID),
		client.DeviceID,
		[]byte("argocd"),
	)

	err = cryptoStore.Upgrade()
	if err != nil {
		return nil, fmt.Errorf("couldn't upgrade crypto store tables: %w", err)
	}

	olmMachine := crypto.NewOlmMachine(client, cryptoLogger, cryptoStore, store)
	err = olmMachine.Load()
	if err != nil {
		return nil, fmt.Errorf("couldn't load olm machine: %w", err)
	}

	syncer := client.Syncer.(*mautrix.DefaultSyncer)
	syncer.OnSync(olmMachine.ProcessSyncResponse)
	syncer.OnEventType(event.StateMember, func(_ mautrix.EventSource, evt *event.Event) {
		olmMachine.HandleMemberEvent(evt)
	})
	syncer.OnEvent(func(_ mautrix.EventSource, evt *event.Event) {
		err := olmMachine.FlushStore()
		if err != nil {
			panic(fmt.Errorf("couldn't flush olm machine store: %w", err))
		}
	})

	return olmMachine, nil
}

type matrixSyncer struct {
	*mautrix.DefaultSyncer
	store *matrixStore
}

func newMatrixSyncer(store *matrixStore) mautrix.Syncer {
	return &matrixSyncer{
		mautrix.NewDefaultSyncer(),
		store,
	}
}

func (s *matrixSyncer) ProcessResponse(res *mautrix.RespSync, since string) error {
	err := s.DefaultSyncer.ProcessResponse(res, since)
	if err != nil {
		return err
	}
	s.store.Save()
	return nil
}

func (s *matrixSyncer) GetFilterJSON(userID id.UserID) *mautrix.Filter {
	all := []event.Type{event.NewEventType("*")}
	noTypes := mautrix.FilterPart{NotTypes: all}
	stateEvtTypes := []event.Type{
		event.StateCreate,
		event.StateEncryption,
		event.StateMember,
	}
	return &mautrix.Filter{
		AccountData: noTypes,
		Presence:    noTypes,
		Room: mautrix.RoomFilter{
			State: mautrix.FilterPart{
				Types: stateEvtTypes,
			},
			Timeline: mautrix.FilterPart{
				Types: stateEvtTypes,
			},
		},
	}
}

const matrixStoreVersion = 0

type matrixStore struct {
	sync.RWMutex

	storePath string `json:"-"`

	FilterIDs   map[id.UserID]string        `json:"-"`
	NextBatches map[id.UserID]string        `json:"next_batches"`
	Rooms       map[id.RoomID]*mautrix.Room `json:"rooms"`
	Version     int                         `json:"version"`
}

func newMatrixStore(dataPath string) (*matrixStore, error) {
	storePath := path.Join(dataPath, "store.json")

	store := &matrixStore{
		sync.RWMutex{},
		storePath,
		make(map[id.UserID]string),
		make(map[id.UserID]string),
		make(map[id.RoomID]*mautrix.Room),
		-1,
	}

	f, err := os.Open(storePath)
	if err != nil {
		return nil, fmt.Errorf("couldn't open file %s for reading matrix store: %w", storePath, err)
	}
	err = json.NewDecoder(f).Decode(&store)
	if err != nil {
		return nil, fmt.Errorf("couldn't decode matrix store when reading file %s: %w", storePath, err)
	}

	// this code is either too new or too old for the store on disk
	if store.Version != matrixStoreVersion {
		log.Warnf("the matrix store at path %s is version %d but this version of argo only supports %d; doing a full sync...", storePath)

		store = &matrixStore{
			sync.RWMutex{},
			storePath,
			make(map[id.UserID]string),
			make(map[id.UserID]string),
			make(map[id.RoomID]*mautrix.Room),
			matrixStoreVersion,
		}
	}

	return store, nil
}

func (s *matrixStore) Save() {
	f, err := os.OpenFile(s.storePath, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0644)
	if err != nil {
		log.Errorf("couldn't open file %s for writing matrix store: %v", s.storePath, err)
		return
	}
	defer f.Close()
	err = json.NewEncoder(f).Encode(s)
	if err != nil {
		log.Errorf("couldn't encode matrix store when writing to file %s: %v", s.storePath, err)
		return
	}
}

// mautrix.Storer interface implemented below

func (s *matrixStore) SaveFilterID(userID id.UserID, filterID string) {
	s.Lock()
	defer s.Unlock()
	s.FilterIDs[userID] = filterID
}

func (s *matrixStore) LoadFilterID(userID id.UserID) string {
	s.RLock()
	defer s.RUnlock()
	return s.FilterIDs[userID]
}

func (s *matrixStore) SaveNextBatch(userID id.UserID, nextBatchToken string) {
	s.Lock()
	defer s.Unlock()
	if s.NextBatches[userID] == nextBatchToken {
		return
	}
	s.NextBatches[userID] = nextBatchToken
}

func (s *matrixStore) LoadNextBatch(userID id.UserID) string {
	s.RLock()
	defer s.RUnlock()
	return s.NextBatches[userID]
}

func (s *matrixStore) SaveRoom(room *mautrix.Room) {
	s.Lock()
	defer s.Unlock()
	s.Rooms[room.ID] = room
}

func (s *matrixStore) LoadRoom(roomID id.RoomID) *mautrix.Room {
	s.RLock()
	defer s.RUnlock()
	return s.Rooms[roomID]
}

func (s *matrixStore) UpdateState(_ mautrix.EventSource, evt *event.Event) {
	if !evt.Type.IsState() {
		return
	}
	room := s.LoadRoom(evt.RoomID)
	if room == nil {
		room = mautrix.NewRoom(evt.RoomID)
		s.SaveRoom(room)
	}
	room.UpdateState(evt)
}

// crypto.StateStore interface implemented below

// IsEncrypted returns whether a room is encrypted.
func (s *matrixStore) IsEncrypted(roomID id.RoomID) bool {
	s.RLock()
	defer s.RUnlock()
	if room, exists := s.Rooms[roomID]; exists {
		return room.GetStateEvent(event.StateEncryption, "") != nil
	}
	return false
}

// GetEncryptionEvent returns the encryption event's content for an encrypted room.
func (s *matrixStore) GetEncryptionEvent(roomID id.RoomID) *event.EncryptionEventContent {
	s.RLock()
	defer s.RUnlock()
	room, exists := s.Rooms[roomID]
	if !exists {
		return nil
	}
	evt := room.GetStateEvent(event.StateEncryption, "")
	content, ok := evt.Content.Parsed.(*event.EncryptionEventContent)
	if !ok {
		return nil
	}
	return content
}

// FindSharedRooms returns the encrypted rooms that another user is also in for a user ID.
func (s *matrixStore) FindSharedRooms(userID id.UserID) []id.RoomID {
	s.RLock()
	defer s.RUnlock()
	var sharedRooms []id.RoomID
	for roomID, room := range s.Rooms {
		// if room isn't encrypted, skip
		if room.GetStateEvent(event.StateEncryption, "") == nil {
			continue
		}
		if room.GetMembershipState(userID) == event.MembershipJoin {
			sharedRooms = append(sharedRooms, roomID)
		}
	}
	return sharedRooms
}

func (s *matrixStore) GetRoomMembers(roomID id.RoomID) []id.UserID {
	var members []id.UserID
	for userID, evt := range s.Rooms[roomID].State[event.StateMember] {
		if evt.Content.Parsed.(*event.MemberEventContent).Membership.IsInviteOrJoin() {
			members = append(members, id.UserID(userID))
		}
	}
	return members
}

type matrixCryptoLogger struct{}

func (f matrixCryptoLogger) Error(message string, args ...interface{}) {
	log.Errorf(message, args...)
}

func (f matrixCryptoLogger) Warn(message string, args ...interface{}) {
	log.Warnf(message, args...)
}

func (f matrixCryptoLogger) Debug(message string, args ...interface{}) {
	log.Debugf(message, args...)
}

func (f matrixCryptoLogger) Trace(message string, args ...interface{}) {
	log.Tracef(message, args...)
}
