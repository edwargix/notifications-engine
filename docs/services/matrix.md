# Matrix

**NOTE:** native end-to-end encryption (e2ee) for Matrix notifications is not yet supported because CGO is not supported by Argo.  Those who want end-to-end encryption support for their Argo notifications bot can setup [pantalaimon](https://github.com/matrix-org/pantalaimon).

To be able to send notifications via Matrix, do the following steps:

1. Register a Matrix account
2. Generate an access token and device ID using the account
3. Upload a profile picture (optional)
4. Configure notifiers and subscription recipients

## Register a Matrix account

Registering a Matrix account can be done via a standard Matrix client like
[Element](https://element.io) (or others: <https://matrix.org/clients>).  It can
also be done via `curl`, but this can be difficult for homeservers that require
multiple steps for registrations (CAPTCHAs, etc).  However, if your homeserver
is running Synapse and you have access to the `registration_shared_secret`, you
can register a new user with the endpoint explained here:
<https://matrix-org.github.io/synapse/latest/admin_api/register_api.html>

## Generate an access token and device ID using the account

For example, suppose your user ID is `@foo:example.org`.  Then, you'd use your server name in

```sh
# set this to your server name; e.g. matrix.org
export SERVER_NAME="example.org"
export HOMESERVER_URL=$(curl -LSs https://${SERVER_NAME}/.well-known/matrix/client | jq -r '."m.homeserver"."base_url"')
# replace <user_id> with your user ID (@localpart:server.tld) and
# <password> with the password set during registration
RESP=`curl -d '{"type": "m.login.password", "identifier": {"type": "m.id.user", "user": "<user_id>"}, "password": "<password>"}' -X POST $HOMESERVER_URL/_matrix/client/v3/login`
```

## Configure notifiers and subscription recipients

The Matrix notification service send matrix

* `accessToken` - the access token retrieved after logging in
* `deviceID` - the device ID.  Retrieved alongside the access token
* `homeserverURL` - optional, the homeserver base URL.  If unspecified, the base URL will be retrieved using the [well-known URI](https://spec.matrix.org/v1.3/client-server-api/#well-known-uri), if possible
* `userID` - the user ID.  Retrieved alongside the access token.  Of the form `@localpart:server.tld`
