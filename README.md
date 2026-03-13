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
- Both return the token **once** and revoke any previous token for the same scope.
- Tokens do not expire automatically. Use owner JWT to call these endpoints.

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

### Mission History
- `POST /missions/{id}/start` will insert a record into `mission_history` with status `InProgress`.
- `POST /mission-history/{history_id}/complete` will update that history row to `Completed`.
- `GET /mission-history/me` returns history entries for the authenticated user (paginated).

Start Response:
```json
{
  "history_id": 123,
  "status": "InProgress"
}
```

Complete Request:
`POST /mission-history/123/complete`

Typical flow:
1. Call `POST /missions/{id}/start` → store `history_id`.
2. Fly mission + upload media (use `history_id` + `mission_id`).
3. Call `POST /mission-history/{history_id}/complete`.

List history (paginated):
```
GET /mission-history/me?page=1&limit=20
```
Notes:
- Default `page=1`, `limit=20`
- Max `limit=100`
- Optional filter: `mission_id`

Fields:
- `started_at`: time when `start` was called.
- `completed_at`: time when `complete` was called (null while `InProgress`).

### Upload Media (image/video, optional mission_id)
- `POST /upload-footage` (multipart form)
Fields:
- `uav_id` (required)
- `mission_id` (optional, link media to mission history)
- `history_id` (required if `mission_id` is provided)
- `file` (required)
Notes:
- Accepted image types: `.jpg`, `.jpeg`, `.png`, `.webp`.
- Accepted video types: `.mp4`, `.m4`, `.m4v`.
- Each upload is also appended to `mission_history_media`.

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
