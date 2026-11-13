# Slack integration

Record turnbacks without leaving Slack:

```text
/turnback vpn 10 disconnected again     → 10 minutes against the problem with alias “vpn”, with a note
/turnback ci 2h                         → durations accept m/min/h suffixes
/turnback vpn 10 x15 all day            → 15 occurrences of 10 minutes (x30! confirms over 20)
/turnback devenv 180! rebuild           → “!” confirms an unusually long duration
/turnback list                          → show problem aliases
/turnback help
```

The web application is fully functional without Slack; the integration is enabled only when a signing
secret is configured. Mattermost and Microsoft Teams use the same commands through inbound webhooks, and
Slack channels can receive notifications through outbound webhooks ([webhooks.md](webhooks.md)).

## Setup

1. Create a Slack app (*From scratch*) in your workspace.
2. **Slash Commands → Create New Command**: command `/turnback`,
   request URL `https://turnbacks.example.com/api/v1/integrations/slack/commands`,
   usage hint `vpn 10 disconnected again`.
3. **Basic Information → App Credentials → Signing Secret** → set
   `TURNBACKS_SLACK_SIGNING_SECRET` (or `_FILE`).
4. Install the app to the workspace. Give problems short aliases (Problems → Edit) so they are easy to type.

The Turnbacks server must be reachable from Slack over HTTPS.

## Security model

- **Signature verification**: each request's `X-Slack-Signature` (HMAC-SHA256 of
  `v0:{timestamp}:{raw body}`) is verified with the signing secret using a constant-time comparison.
- **Freshness**: requests whose `X-Slack-Request-Timestamp` is more than five minutes from server time
  are rejected.
- **Replay rejection**: the hash of every accepted signature is stored for 15 minutes; a repeated
  signature is rejected with `401`. Turnback IDs are derived deterministically from the signature, so
  even a replay that slipped through would be idempotent.
- **Identity**: no account linking is needed. The reporter is recorded as `slack:<team>/<user>`, so
  Slack reports count towards "people affected" (each Slack user is one person); like IP addresses,
  it is visible to administrators only.
- **Validation**: Slack submissions go through the same service methods and validation as the web API.
  The `source` is recorded as `slack`.

## Contract

`POST /api/v1/integrations/slack/commands` with Slack's standard `application/x-www-form-urlencoded`
payload (`team_id`, `user_id`, `command`, `text`, …). Responses are ephemeral Slack messages:

```json
{ "response_type": "ephemeral", "text": "✅ Recorded 10 min against *Corporate VPN disconnects*. Thanks — it all adds up." }
```

`401` is returned for invalid, stale or replayed signatures. Validation problems (unknown alias, excessive
duration without `!`, archived problem) are returned as ephemeral messages with status `200`, as Slack
expects.

## Possible later enhancements

Interactive modals (pick a problem from a menu), a global shortcut, and DM confirmations were deliberately
left out of the MVP; the slash command covers the ten-second recording goal. The command parser is shared
with Mattermost and Teams (`internal/integrations/chatcmd`), so improvements apply to all three.
