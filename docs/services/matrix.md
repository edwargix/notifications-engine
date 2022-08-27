# Matrix

To be able to send Matrix messages.

1. make sure you have a Mtarix account
2. generate an access token for the account

## Creating a Matrix account

```sh

```

## Parameters

The Matrix notification service send matrix

* `accessToken` - the access token retrieved after logging in
* `deviceID` - the device ID.  Retrieved alongside the access token
* `homeserverURL` - optional, the homeserver base URL.  If unspecified, the base
  URL will be retrieved using the [well-known
  URI](https://spec.matrix.org/v1.3/client-server-api/#well-known-uri), if
  possible
* `userID` - the user ID.  Retrieved alongside the access token

## Generating an Access Token

An access token can
