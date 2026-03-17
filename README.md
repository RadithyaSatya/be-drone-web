# XFlight Backend API

The XFlight Backend is a **Go**-based RESTful API designed to manage autonomous UAV (Unmanned Aerial Vehicle) missions. It serves as the central bridge between a XFlight-UAV client and a Kotlin-based application, also persistent PostgreSQL database, handling mission scheduling, waypoint management, telemetry logging, and video footage uploads.

Key Components:
- Mission Monitor: Polls the API for missions with a Waiting status.
- Scheduler: Delays execution until the mission's scheduled_time is reached.
- MAVLink Interface: Communicates with the drone (ArduPilot/PX4) via UDP.

---

## 🚀 System Architecture


| Status       | Meaning                                  |
|--------------|-------------------------------------------|
| **Mission Lifecycle**      | Creating missions with specific waypoints and tracking their status (Waiting, Scheduled, etc.).      |
| **Real-time Data**     | Receiving batch telemetry logs (Battery, GPS, Altitude) from active UAVs.             |
| **Asset Management** |  Storing and serving drone video footage.          |




## 📡 API Endpoints

Import the Postman collection to analyze and test all of the available endpoints

[View Postman Collection](https://rishaldy7-8367785.postman.co/workspace/rishaldy-7's-Workspace~acebe5a2-c112-41c1-84f4-3b3a4ed315e0/collection/50620325-c209fda0-c9b4-49c4-9be1-8fe051320cf7?action=share&creator=50620325)

---

## UAV Dropdown

Use `GET /uavs/me/dropdown` to populate lightweight UAV selectors in the frontend.

Each item contains:
- `id`
- `name`
- `camera_spec`
- `primary_docking` (optional)

`primary_docking` is nullable and represents the UAV's primary active docking, resolved by `is_primary DESC, created_at DESC`. If a UAV has no active docking, the field is omitted.

Example response:
```json
[
  {
    "id": 1,
    "name": "UAV Alpha",
    "camera_spec": "4K",
    "primary_docking": {
      "id": 5,
      "name": "Dock A",
      "location_name": "Hangar Timur",
      "latitude": -6.2,
      "longitude": 106.8
    }
  }
]
```

---

## ⚡ Realtime Telemetry (HTTP -> WebSocket)

**Goal**: device services, gateways, or drones send realtime data to the backend, and the backend forwards that data to frontend clients over WebSocket.

### Realtime Architecture

There is only one realtime WebSocket endpoint:
- `GET /ws/telemetry`

All realtime messages flow through that endpoint. The backend does not expose separate WebSocket topics or rooms. Instead, each client sends a `subscribe` message with one or more `uav_id` values, and the backend only forwards events whose `uav_id` matches that subscription list.

In short:
1. A producer sends data by HTTP `POST /realtime/telemetry` or by WebSocket `type=publish`.
2. The backend validates and normalizes the message.
3. The backend pushes the message into the realtime hub.
4. The hub forwards the message to WS clients subscribed to the same `uav_id`.

### Who Usually Uses Which Flow

- **Frontend dashboard**: usually `connect -> subscribe -> receive`
- **Drone or gateway that only sends telemetry**: usually `connect -> publish`
- **Client that both sends and listens**: `connect -> subscribe -> publish`

Important:
- A publisher-only WebSocket client does **not** need to send `subscribe` first.
- A subscriber-only client does **not** need to send `publish`.
- Subscription filtering is based on `uav_id` only, not by `metric`.

### Authentication Flow

All normal API endpoints except `/auth/login` accept one of these:
```text
Authorization: Bearer <JWT>
```
or
```text
X-Device-Token: <DEVICE_TOKEN>
```

WebSocket authentication works differently:
1. Authenticate first with JWT or device token.
2. Request a short-lived WS token from `POST /auth/ws-token`.
3. Open the socket with `GET /ws/telemetry?token=<WS_TOKEN>`.

Notes:
- The WS token is a JWT with audience `ws`.
- Default WS token TTL is `120` seconds, configurable by `WS_TOKEN_TTL_SECONDS`.
- Device tokens are stored in `device_tokens` and scoped to either a UAV or a docking.
- Store device tokens as `SHA256(<token> + DEVICE_TOKEN_PEPPER)` in `device_tokens.token_hash`.

### End-to-End Flow

#### 1. Subscriber-only flow

Use this for frontend screens that only need to receive updates.

1. Call `POST /auth/login` or authenticate with a device token.
2. Call `POST /auth/ws-token`.
3. Connect to `GET /ws/telemetry?token=<WS_TOKEN>`.
4. Send a subscribe message:

```json
{
  "type": "subscribe",
  "uav_ids": [2, 3]
}
```

5. Receive all realtime messages for UAV `2` and `3`.

Behavior notes:
- If you never send `subscribe`, the connection stays open but receives no realtime events.
- Sending `subscribe` again replaces the previous list. It does not append to it.
- Invalid or non-positive `uav_id` values are ignored.

#### 2. Publisher-only flow

Use this for drones or gateways that only need to send telemetry.

1. Authenticate and request `POST /auth/ws-token`.
2. Connect to `GET /ws/telemetry?token=<WS_TOKEN>`.
3. Send publish messages immediately. No `subscribe` message is required.

Example:
```json
{
  "type": "publish",
  "uav_id": 2,
  "kind": "telemetry",
  "metric": "battery",
  "payload": {
    "percent": 78.2,
    "voltage": 15.6
  }
}
```

Behavior notes:
- `type=publish` over WebSocket currently supports only `kind=telemetry`.
- If `kind` is omitted, the backend treats it as `telemetry`.
- Status messages such as `uav_status` or `docking_status` must still use `POST /realtime/telemetry`.

#### 3. HTTP ingestion -> WebSocket fanout flow

Use this when devices prefer HTTP for sending data and frontend still needs realtime updates.

1. Device sends `POST /realtime/telemetry`.
2. Backend validates the payload.
3. Backend broadcasts the resulting message to matching WS subscribers.

This is the standard request body:
```json
{
  "uav_id": 2,
  "kind": "telemetry",
  "metric": "battery",
  "payload": {
    "percent": 78.2,
    "voltage": 15.6
  }
}
```

Field meanings:
- `uav_id` (required): UAV primary key from table `uav`
- `kind` (required): `telemetry` or `status`
- `metric` (required):
  - for `kind=telemetry`, this is the telemetry channel name such as `battery`, `location`, or `imu`
  - for `kind=status`, use `uav_status` or `docking_status`
- `payload` (required): JSON object containing channel-specific data

Behavior notes:
- For `kind=telemetry`, the backend broadcasts the message and does not persist the telemetry payload to the database.
- For `kind=status`, the backend upserts `uav_status` or `docking_status`, updates `last_heartbeat`, then broadcasts the resulting status snapshot.
- For `metric=uav_status`, `uav_id` must be the UAV `id`.
- For `metric=docking_status`, the backend resolves which docking row to update:
  - with a docking-scoped device token, it updates that docking
  - with a UAV-scoped token or owner JWT, include `docking_id` in `payload` to target a specific docking for that UAV
  - if `docking_id` is omitted, the backend falls back to the primary active docking for that UAV

### WebSocket Client Messages

After the WebSocket is open, the backend expects JSON messages from the client.

#### Subscribe message

```json
{
  "type": "subscribe",
  "uav_ids": [2, 3]
}
```

Meaning:
- Start receiving every realtime message for UAV `2` and `3`
- Replace any previous subscription list for this connection

#### Publish message

```json
{
  "type": "publish",
  "uav_id": 2,
  "kind": "telemetry",
  "metric": "battery",
  "payload": {
    "percent": 78.2,
    "voltage": 15.6
  }
}
```

Meaning:
- Push one realtime telemetry event into the backend hub
- The backend fans it out to subscribers of that `uav_id`

### Message Format Sent to WebSocket Subscribers

Every realtime event sent by the backend follows this shape:

```json
{
  "uav_id": 2,
  "kind": "telemetry",
  "metric": "battery",
  "ts": "2026-02-05T10:00:00Z",
  "payload": {
    "percent": 78.2,
    "voltage": 15.6
  }
}
```

Status messages use the same envelope:

```json
{
  "uav_id": 3,
  "kind": "status",
  "metric": "uav_status",
  "ts": "2026-02-05T10:01:00Z",
  "payload": {
    "battery_percent": 80,
    "is_in_flight": false,
    "is_docked": true,
    "last_heartbeat": "2026-02-05T10:01:00Z"
  }
}
```

Envelope fields:
- `uav_id`: the UAV this event belongs to
- `kind`: `telemetry` or `status`
- `metric`: the channel name or status type
- `ts`: server-side UTC timestamp when the event was emitted
- `payload`: event-specific JSON object

### Delivery Rules and Current Behavior

- One WS endpoint serves all realtime traffic: `/ws/telemetry`
- Event routing is filtered by `uav_id` only
- Subscribing to a UAV means receiving all metrics and all status events for that UAV
- Publisher-only clients can publish immediately after the socket opens
- There is no success acknowledgement frame for `subscribe` or `publish`
- Invalid WS messages are ignored and logged server-side
- `publish` over WS supports only telemetry, not status
- Telemetry and status can both be broadcast to subscribers
- Telemetry is not stored by the realtime handler; status is upserted before broadcast

### Connection Lifecycle Notes

- The backend sends periodic WebSocket ping frames to keep the connection alive
- Maximum inbound WS message size is `64 KB`
- Each client connection has a bounded outbound buffer; if the client is too slow, the backend may close the connection
- Reconnect logic should be implemented client-side

### Device Tokens (per device)
- `POST /device-tokens/uav/{uav_id}` generates a token scoped to a UAV.
- `POST /device-tokens/docking/{docking_id}` generates a token scoped to a docking.
- `GET /device-context` returns the device identity derived from `X-Device-Token`.
- Both return the token **once** and revoke any previous token for the same scope.
- Tokens do not expire automatically. Use owner JWT to call these endpoints.

Example `GET /device-context` response for a UAV token:
```json
{
  "token_id": 12,
  "scope_type": "uav",
  "uav_id": 1,
  "resolved_uav_id": 1
}
```

Example `GET /device-context` response for a docking token:
```json
{
  "token_id": 18,
  "scope_type": "docking",
  "docking_id": 5,
  "resolved_uav_id": 1
}
```

Notes:
- `resolved_uav_id` is always the UAV that the backend will use for device mission polling.
- For docking services, `GET /device-context` is the recommended startup/bootstrap call if the service still needs `uav_id` or `docking_id` for telemetry payloads or local state.
- If the request is authenticated with JWT instead of `X-Device-Token`, the endpoint returns `401 device token required`.

### Login (JWT issuer, users table)
- `POST /auth/login`
```json
{
  "username": "admin",
  "password": "admin123"
}
```

Response:
```json
{
  "token": "eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9..."
}
```
Notes:
- Credentials are validated against the `users` table.
- Passwords are stored as bcrypt hashes by default.

### Add User (JWT or Device Token)
- `POST /register-user`
```
Authorization: Bearer <JWT>
```
or
```
X-Device-Token: <DEVICE_TOKEN>
```
```json
{
  "email": "user@example.com",
  "dob": "1990-01-01",
  "phone": "+628123456789",
  "username": "user1",
  "pilot_cert": "CERT-001",
  "password": "secret123"
}
```
Notes:
- `password` or `password_hash` is required.
- `dob` must be `YYYY-MM-DD` when provided.
- `password` will be hashed with bcrypt.
- `password_hash` must be bcrypt (preferred) or legacy `sha256:<salt>:<hash>`.

### Update My Profile (JWT only)
- `PATCH /users/me`
```
Authorization: Bearer <JWT>
```
```json
{
  "email": "new@example.com",
  "username": "user1",
  "dob": "1990-01-01",
  "phone": "+628123456789",
  "pilot_cert": "CERT-001"
}
```
Response:
```json
{
  "id": 1,
  "email": "new@example.com",
  "username": "user1",
  "dob": "1990-01-01T00:00:00Z",
  "phone": "+628123456789",
  "pilot_cert": "CERT-001",
  "created_at": "2026-02-01T12:00:00Z"
}
```
Notes:
- Send only fields you want to update.
- `dob` must be `YYYY-MM-DD` when provided.
- Send empty string to clear `dob`, `phone`, or `pilot_cert`.
- If you change `username`, login again to get a new JWT.

### Get My Profile (JWT only)
- `GET /users/me`
```
Authorization: Bearer <JWT>
```
Response:
```json
{
  "id": 1,
  "email": "user@example.com",
  "username": "user1",
  "dob": "1990-01-01T00:00:00Z",
  "phone": "+628123456789",
  "pilot_cert": "CERT-001",
  "created_at": "2026-02-01T12:00:00Z"
}
```
Notes:
- `dob`, `phone`, `pilot_cert` will be omitted if null.

### Change My Password (JWT only)
- `PATCH /users/me/password`
```
Authorization: Bearer <JWT>
```
```json
{
  "current_password": "oldpass123",
  "new_password": "newpass123"
}
```
Response:
```json
{
  "message": "Password updated successfully"
}
```
Notes:
- `current_password` dan `new_password` wajib.
- Jika `current_password` salah akan `401`.

### Realtime Telemetry (JWT or Device Token)
- `POST /realtime/telemetry`
```
X-Device-Token: <DEVICE_TOKEN>
```
or
```
Authorization: Bearer <JWT>
```

### WS Token (short-lived)
- `POST /auth/ws-token`
```
Authorization: Bearer <JWT>
```
or
```
X-Device-Token: <DEVICE_TOKEN>
```

Response:
```json
{
  "token": "eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9...",
  "expires_in": 120
}
```

### Subscribe from Frontend (WS)
After connecting, the client sends:
```json
{
  "type": "subscribe",
  "uav_ids": [2, 3]
}
```
The backend will only push data for UAVs in this list.

### End-to-End Example

**1) Send status via HTTP**
```bash
curl -X POST http://127.0.0.1:8080/realtime/telemetry \
  -H "Content-Type: application/json" \
  -H "X-Device-Token: <DEVICE_TOKEN>" \
  -d '{
    "uav_id":2,
    "kind":"status",
    "metric":"docking_status",
    "payload":{"is_online":false}
  }'
```

**2) WS Subscribe**
```json
{"type":"subscribe","uav_ids":[2]}
```

**3) WS Receive**
```json
{
  "uav_id": 2,
  "kind": "status",
  "metric": "docking_status",
  "ts": "2026-02-04T06:25:48.885371Z",
  "payload": {
    "is_online": true
  }
}
```

### Docking (Create/Update/Delete)
- `POST /dockings` create docking for a UAV.
- `PATCH /dockings/{id}` update docking fields (no `uav_id` update).
- `DELETE /dockings/{id}` delete docking.
No docking list/get endpoints; docking data is returned via `GET /uavs/{id}`.

### UAV Details
- `GET /uavs/{id}` returns UAV info with its docking list.
- Each docking includes its latest `docking_status` (if available).

### Mission CRUD
- `GET /missions/me` returns missions for the authenticated user with pagination.
- `GET /missions/{id}` returns one mission with its waypoint list.
- `POST /register-mission` creates a mission.
- `PATCH /missions/{id}` updates a mission and optionally replaces its waypoint list.
- `DELETE /missions/{id}` soft-deletes a mission.

Mission list query params:
- `page` default `1`
- `limit` default `20`, max `100`
- `uav_id` optional filter
- `date` optional exact schedule date in `YYYY-MM-DD`

Example `GET /missions/me?page=1&limit=20&date=2026-03-14` response:
```json
{
  "page": 1,
  "limit": 20,
  "total": 1,
  "total_pages": 1,
  "has_next": false,
  "has_prev": false,
  "next_page": null,
  "prev_page": null,
  "items": [
    {
      "id": 10,
      "user_id": 3,
      "uav_id": 2,
      "mission_name": "Survey Alpha",
      "schedule": "2026-03-14T08:00:00Z",
      "is_recurring": true,
      "recurrence_unit": "hour",
      "recurrence_interval": 4,
      "status": "Waiting",
      "timestamp": "2026-03-13T09:00:00Z",
      "deleted_at": null,
      "waypoint_count": 3,
      "uav": {
        "id": 2,
        "name": "Drone Alpha"
      }
    }
  ]
}
```

Example `GET /missions/{id}` response:
```json
{
  "id": 10,
  "user_id": 3,
  "uav_id": 2,
  "mission_name": "Survey Alpha",
  "schedule": "2026-03-14T08:00:00Z",
  "is_recurring": true,
  "recurrence_unit": "hour",
  "recurrence_interval": 4,
  "status": "Waiting",
  "timestamp": "2026-03-13T09:00:00Z",
  "waypoints": [
    {
      "id": 1,
      "sequence_order": 1,
      "latitude": -6.2,
      "longitude": 106.8,
      "altitude": 30,
      "action": "take_photo",
      "action_duration": 5
    }
  ]
}
```

Example `PATCH /missions/{id}` body:
```json
{
  "mission_name": "Survey Alpha Revised",
  "schedule": "2026-03-14T12:00:00Z",
  "is_recurring": true,
  "recurrence_unit": "hour",
  "recurrence_interval": 6,
  "status": "Waiting",
  "waypoints": [
    {
      "sequence_order": 1,
      "latitude": -6.2,
      "longitude": 106.8,
      "altitude": 40,
      "action": "hold",
      "action_duration": 10
    }
  ]
}
```

Mission mutation rules:
- Update and delete require owner JWT.
- Update and delete are rejected with `409` if the mission currently has an active run (`InProgress` / non-terminal history).
- If `waypoints` is provided on update, the backend replaces all existing waypoints with the new list.
- If `waypoints` is omitted on update, existing waypoints are kept.

### Mission History
- Mission polling for device services is split into two recommended endpoints:
  - `GET /missions/waiting/device` for docking services looking for the next `Waiting` mission
  - `GET /missions/safe-to-fly/device` for drone services looking for the active `SafeToFly` mission run
- `GET /device-context` can be used once at service startup to discover the IDs associated with the current device token.
- `POST /missions/{id}/start` creates a mission run in `mission_history` with initial status `PreparingDock`.
- `GET /mission-history/{history_id}/state` returns the current runtime state for one mission run.
- `PATCH /mission-history/{history_id}/state` moves the mission run through its runtime states.
- `POST /mission-history/{history_id}/complete` finalizes the mission run after `DockConfirmed`.
- `GET /mission-history/me` returns history entries for the authenticated user (paginated).
- `GET /mission-history/{history_id}/events` returns the event timeline for one mission run.

Mission polling responses:
- Both polling endpoints return the same mission fields and waypoint list as the old `GET /missions/next/{user_id}` flow.
- `GET /missions/safe-to-fly/device` also includes:
  - `history_id`
  - `runtime_status`
- Both device polling endpoints derive the UAV from `X-Device-Token`:
  - UAV token -> direct `uav_id`
  - docking token -> backend resolves `uav_id` from `docking_id`
- These endpoints are intended for device tokens and do not require `uav_id` in the URL anymore.
- `GET /device-context` returns the same token-derived identity so device services can avoid hardcoded IDs.

Example waiting mission response:
```json
{
  "id": 10,
  "user_id": 3,
  "uav_id": 2,
  "mission_name": "Survey Alpha",
  "schedule": "2026-03-13T10:00:00Z",
  "is_recurring": true,
  "recurrence_unit": "hour",
  "recurrence_interval": 4,
  "status": "Waiting",
  "timestamp": "2026-03-13T09:00:00Z",
  "waypoints": [
    {
      "id": 1,
      "sequence_order": 1,
      "latitude": -6.2,
      "longitude": 106.8,
      "altitude": 30,
      "action": "take_photo",
      "action_duration": 5
    }
  ]
}
```

Example SafeToFly mission response:
```json
{
  "id": 10,
  "user_id": 3,
  "uav_id": 2,
  "mission_name": "Survey Alpha",
  "schedule": "2026-03-13T10:00:00Z",
  "is_recurring": true,
  "recurrence_unit": "hour",
  "recurrence_interval": 4,
  "status": "InProgress",
  "timestamp": "2026-03-13T09:00:00Z",
  "waypoints": [
    {
      "id": 1,
      "sequence_order": 1,
      "latitude": -6.2,
      "longitude": 106.8,
      "altitude": 30,
      "action": "take_photo",
      "action_duration": 5
    }
  ],
  "history_id": 123,
  "runtime_status": "SafeToFly"
}
```

Polling endpoint status codes:
- `200 OK`: mission found
- `404 Not Found`: no matching mission currently available, or docking token points to a docking row that no longer exists
- `403 Forbidden`: device token scope is invalid for mission lookup
- `401 Unauthorized`: missing/invalid device token, or request was not authenticated as a device

Mission scheduling / recurrence:
- `schedule` stores the next execution time for the mission template.
- Non-recurring mission:
  - `is_recurring = false`
  - `recurrence_unit = null`
  - `recurrence_interval = null`
- Recurring mission:
  - `is_recurring = true`
  - `recurrence_unit` supports `hour` or `day`
  - `recurrence_interval` must be greater than `0`
- Backward compatibility:
  - old recurring missions without recurrence fields are treated as `day + 1`

Example recurring mission request:
```json
{
  "uav_id": 1,
  "mission_name": "Dock Patrol",
  "schedule": "2026-03-14T08:00:00Z",
  "is_recurring": true,
  "recurrence_unit": "hour",
  "recurrence_interval": 4,
  "status": "Waiting",
  "waypoints": [
    {
      "sequence_order": 1,
      "latitude": -6.2,
      "longitude": 106.8,
      "altitude": 20,
      "action": "hold",
      "action_duration": 5
    }
  ]
}
```

Runtime states:
- `PreparingDock`
- `SafeToFly`
- `Takeoff`
- `DockConfirmed`
- `Completed`
- `Failed`
- `Aborted`

Normal transition flow:
1. `PreparingDock`
2. `SafeToFly`
3. `Takeoff`
4. `DockConfirmed`
5. `Completed`

Terminal states:
- `Completed`
- `Failed`
- `Aborted`

Transition rules:
- `PreparingDock -> SafeToFly`
- `SafeToFly -> Takeoff`
- `Takeoff -> DockConfirmed`
- `DockConfirmed -> Completed`
- Any non-terminal state can transition to `Failed` or `Aborted`

Automatic recovery:
- The backend runs a background sweeper every minute to close stale non-terminal mission runs automatically.
- `Waiting` missions that stay overdue for more than `1m` are also recovered automatically:
  - recurring missions stay `Waiting` and move directly to the next future `schedule`
  - non-recurring missions are marked `Failed`
- Timeout policy:
  - `PreparingDock` older than `5m` -> `Aborted` with `MISSION_TIMEOUT`
  - `SafeToFly` older than `8m` -> `Aborted` with `MISSION_TIMEOUT`
  - `Takeoff` older than `30m` -> `Failed` with `MISSION_TIMEOUT`
  - `DockConfirmed` older than `5m` without `complete` -> `Failed` with `MISSION_TIMEOUT`
- Recurring missions that end as `Completed`, `Failed`, or `Aborted` are re-queued by setting the mission template back to `Waiting` and recalculating the next schedule.

Start Response:
```json
{
  "history_id": 123,
  "status": "PreparingDock"
}
```

Update state request:
`PATCH /mission-history/123/state`

```json
{
  "status": "SafeToFly",
  "message": "Dock prepared and launch area clear"
}
```

Failure / abort example:
```json
{
  "status": "Failed",
  "failure_code": "BATTERY_LOW",
  "failure_reason": "Battery too low for launch"
}
```

Complete request:
`POST /mission-history/123/complete`

Complete notes:
- `complete` only succeeds when the current mission history status is `DockConfirmed`
- Response `status` is the mission-history runtime status and will be `Completed`
- Response `mission_status` is the mission template status in `missions`
- For recurring missions, `mission_status` returns to `Waiting`
- For recurring missions, the response may also include `next_schedule`
- For recurring missions, `schedule` is recalculated from the recurrence rule:
  - `hour`: add `recurrence_interval` hours until the next future slot
  - `day`: add `recurrence_interval` days until the next future slot

Typical flow:
1. Call `POST /missions/{id}/start` → store `history_id`.
2. Docking updates run to `SafeToFly` with `PATCH /mission-history/{history_id}/state`.
3. Drone updates run to `Takeoff`.
4. Docking detects drone return and updates run to `DockConfirmed`.
5. Call `POST /mission-history/{history_id}/complete`.
6. Upload media to `POST /mission-history/{history_id}/media` as needed.

List history (paginated):
```
GET /mission-history/me?page=1&limit=20
```
Notes:
- Default `page=1`, `limit=20`
- Max `limit=100`
- Optional filter: `mission_id`
- Each item includes `media_count` so the UI can show how many uploaded files belong to that mission run

Fields:
- `started_at`: time when `start` was called.
- `completed_at`: time when the run reached a terminal status (`Completed`, `Failed`, or `Aborted`).
- `media_count`: total number of records in `mission_media` for that `history_id`

Current mission history state:
```text
GET /mission-history/123/state
```

Example response:
```json
{
  "history_id": 123,
  "mission_id": 10,
  "user_id": 3,
  "uav_id": 2,
  "docking_id": 5,
  "status": "SafeToFly",
  "is_terminal": false,
  "failure_code": null,
  "failure_reason": null,
  "completed_at": null,
  "last_event_at": "2026-03-13T10:02:00Z"
}
```

Notes:
- This endpoint is read-only and returns the latest persisted runtime status in `mission_history`.
- Access rules follow the same ownership/device checks as `PATCH /mission-history/{history_id}/state`.
- `last_event_at` is the latest timestamp recorded in `mission_event` for that run, or `null` if no event exists yet.

Mission event timeline:
```text
GET /mission-history/123/events
```

Example response:
```json
{
  "history_id": 123,
  "count": 3,
  "items": [
    {
      "id": 1001,
      "history_id": 123,
      "from_state": null,
      "to_state": "PreparingDock",
      "result": null,
      "failure_code": null,
      "message": null,
      "is_terminal": false,
      "created_at": "2026-03-13T10:00:00Z"
    },
    {
      "id": 1002,
      "history_id": 123,
      "from_state": "PreparingDock",
      "to_state": "SafeToFly",
      "result": null,
      "failure_code": null,
      "message": "Dock prepared and launch area clear",
      "is_terminal": false,
      "created_at": "2026-03-13T10:02:00Z"
    }
  ]
}
```

### Mission Media Uploads
- `POST /mission-history/{history_id}/media` uploads image/video files for one mission run.
- `GET /mission-history/{history_id}/media` lists files already attached to that mission run.

Upload request:
- Content type: `multipart/form-data`
- Supported file fields:
  - `files` for one or more files
  - `file` as a single-file compatibility alias
- Optional text field: `event_id`
  - use this when a file should be attached to a specific `mission_event`
  - omit it when the file belongs to the mission run in general

Behavior:
- Media is attached to `mission_history`, not directly to `missions`
- There is no application-level limit on how many files a mission run can have
- A single upload request may contain one file or multiple files
- Files are stored under `./uploads/footages/<history_id>/...`
- The API returns:
  - `file_path` for server-side/local reference
  - `public_path` for direct public view/stream without auth
  - `download_path` for forced file download without auth

Accepted file types:
- Images: `.jpg`, `.jpeg`, `.png`, `.webp`
- Videos: `.mp4`, `.m4`, `.m4v`

Example upload:
```text
POST /mission-history/123/media
```
Form fields:
- `files`: `launch.jpg`
- `files`: `return.mp4`
- `event_id`: `456` (optional)

Example list:
```text
GET /mission-history/123/media
GET /mission-history/123/media?event_id=456
```

Public media access:
- `GET /footages/...` serves the uploaded image/video without auth
- `GET /mission-media/{media_id}/download` downloads the file as an attachment without auth

### Troubleshooting
- **No WS data**: ensure the client sent the subscribe message.
- **No realtime data**: ensure `POST /realtime/telemetry` payload is valid JSON and includes required fields.
- **401 WS**: missing/invalid WS token.
- **401 Realtime**: missing/invalid auth (`Authorization` or `X-Device-Token`).

### Environment Variables
```
JWT_SECRET=change-me
DEVICE_TOKEN_PEPPER=change-me
```

### Notes
- Realtime frontend channel is only WebSocket `/ws/telemetry`.
- Token JWT currently has no expiry (non-expiring).

## ⚙️ Run


```bash
go run main.go
```

## Setup Instructions
Prerequisites
- Go 1.18+
- PostgreSQL instance
- Directory for uploads: ./uploads/footages (The server creates this automatically on start).

Database Configuration
Update the connStr in main.go with your local credentials:
