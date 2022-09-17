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
is a Synapse instance and you have access to the `registration_shared_secret`,
you can register a new user with the endpoint explained here:
<https://matrix-org.github.io/synapse/latest/admin_api/register_api.html>

## Generate an access token and device ID using the account

Before beginning, ensure you have `curl`, `jq`, and standard unix shell
utilities installed.

If you're a homeserver admin, your homeserver is a Synapse instance, and you
have access to an access token for your admin user, you can use the
[`/_synapse/admin/v1/users/<user_id>/login`
endpoint](https://matrix-org.github.io/synapse/latest/admin_api/user_admin_api.html#login-as-a-user)
to create an access token for your argo user:

<details><summary>Generate token via an existing homeserver admin's token</summary>

Set the environment variables `ADMIN_ACCESS_TOKEN` and `USERID` to the access
token of your admin user and the user ID of your argo user, respectively.

```sh
export ADMIN_ACCESS_TOKEN="ch@ngeMe!"
export USERID="@argocd:example.org"
```

Then, generate an access token for the argo user with

```sh
export SERVER_NAME=$(printf "$USERID" | cut -d: -f2-)
export HOMESERVER_URL=$(curl -LSs https://${SERVER_NAME}/.well-known/matrix/client | jq -r '."m.homeserver"."base_url"')

RESP=`curl -d '{}' $HOMESERVER_URL/_synapse/admin/v1/users/$USERID/login`

echo "Access Token: `printf \"$RESP\" | jq -r .access_token`"
```

</details>

If you're not a homeserver admin, do the following:

Set the environment variables `USERID` and `PASSWORD` to your argo user's
ID and password, respectively:

```sh
# your argo user's ID.  Of the form "@localpart:domain.tld"
export USERID="@argocd:example.org"
# set this to the password for your argo user.  If you need to use a different
# authentication method, the commands in this guide won't work
export PASSWORD="ch@ngeMe!"
```

Then, run the following commands:

```sh
export SERVER_NAME=$(printf "$USERID" | cut -d: -f2-)
export HOMESERVER_URL=$(curl -LSs https://${SERVER_NAME}/.well-known/matrix/client | jq -r '."m.homeserver"."base_url"')
RESP=`curl -d "{\"type\": \"m.login.password\", \"identifier\": {\"type\": \"m.id.user\", \"user\": \"$USERID\"}, \"password\": \"$PASSWORD\"}" -X POST $HOMESERVER_URL/_matrix/client/v3/login`

echo "Access Token: `printf \"$RESP\" | jq -r .access_token`"
echo "Device ID: `printf \"$RESP\" | jq -r .device_id`"
```

You can now use the the Access Token and Device ID printed in the last command
as the respective parameters in the next section.

## Configure notifiers and subscription recipients

The Matrix notification service send matrix

* `accessToken` - the access token retrieved after logging in
* `deviceID` - the device ID.  Retrieved alongside the access token
* `homeserverURL` - optional, the homeserver base URL.  If unspecified, the base URL will be retrieved using the [well-known URI](https://spec.matrix.org/v1.3/client-server-api/#well-known-uri), if possible
* `userID` - the user ID.  Of the form `@localpart:server.tld`
