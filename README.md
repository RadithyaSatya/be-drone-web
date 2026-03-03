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

## ⚡ Realtime Telemetry (HTTP → WebSocket)

**Goal**: Device gateways/services send realtime telemetry to backend HTTP endpoints, and the backend forwards it to frontend clients via WebSocket.

### Data Flow (short)
1. A producer sends telemetry to `POST /realtime/telemetry`.
2. Backend validates and normalizes the request body.
3. Backend pushes the message to WS clients subscribed to the same `drone_id`.
4. Frontend receives realtime updates on `/ws/telemetry`.

### HTTP Ingestion Contract
Endpoint:
- `POST /realtime/telemetry`

Request JSON:
```json
{
  "drone_id": "DRN-001",
  "kind": "telemetry",
  "metric": "battery",
  "payload": {
    "percent": 78.2,
    "voltage": 15.6
  }
}
```

Fields:
- `drone_id` (required)
- `kind` (required): `telemetry` or `status`
- `metric` (required)
Use `metric=uav_status` or `metric=docking_status` when `kind=status`.
For `kind=telemetry`, `metric` represents the telemetry channel name (`battery`, `location`, `imu`, etc.).
- `payload` (required): object
For `kind=status`, backend **upserts** `uav_status` or `docking_status` and updates `last_heartbeat`.
For `metric=uav_status`, `drone_id` should be the UAV `id` or `serial_number`.
For `metric=docking_status`, the backend updates the docking tied to the device token.
If you use a UAV token/JWT, send `docking_id` (must belong to `drone_id`) to target a specific docking; otherwise it falls back to the primary active docking.

### WebSocket Message Mapping
When the backend accepts ingestion, it emits:
```json
{
  "drone_id": "DRN-001",
  "kind": "telemetry",
  "metric": "battery",
  "ts": "2026-02-05T10:00:00Z",
  "payload": {
    "percent": 78.2,
    "voltage": 15.6
  }
}
```

For status:
```json
{
  "drone_id": "3",
  "kind": "status",
  "metric": "uav_status",
  "ts": "2026-02-05T10:01:00Z",
  "payload": {
    "battery_percent": 80,
    "is_connected": true,
    "is_in_flight": false,
    "is_docked": true,
    "last_heartbeat": "2026-02-05T10:01:00Z"
  }
}
```

`metric` identifies the status type or telemetry channel.

### WebSocket Endpoint
- `GET /ws/telemetry`

### Auth (JWT or Device Token)
All API endpoints (except `/auth/login`) accept either:
```
Authorization: Bearer <JWT>
```
or
```
X-Device-Token: <DEVICE_TOKEN>
```

WS clients must authenticate with:
- `?token=<WS_TOKEN>` query param where `WS_TOKEN` is issued by `POST /auth/ws-token`

Device tokens are stored in `device_tokens` and scoped to either a UAV or a docking.
Each request uses the per-device token in `X-Device-Token` (not a global shared token).
Provisioning: store `SHA256(<token> + DEVICE_TOKEN_PEPPER)` (hex) in `device_tokens.token_hash` with the correct scope.

### Device Tokens (per device)
- `POST /device-tokens/uav/{uav_id}` generates a token scoped to a UAV.
- `POST /device-tokens/docking/{docking_id}` generates a token scoped to a docking.
Both return the token **once** and revoke any previous token for the same scope.
Tokens do not expire automatically. Use owner JWT to call these endpoints.

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

### Bootstrap Default User
- `POST /bootstrap/default-user`
```json
{
  "key": "change-me"
}
```
Defaults created by this endpoint:
- `username`: `default`
- `password`: `change-me`
- `email`: `default@example.com`
Note: call once; subsequent calls return 409.

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
  "drones": ["DRN-001", "DRN-002"]
}
```
The backend will only push data for drones in this list.

### End-to-End Example

**1) Send status via HTTP**
```bash
curl -X POST http://127.0.0.1:8080/realtime/telemetry \
  -H "Content-Type: application/json" \
  -H "X-Device-Token: <DEVICE_TOKEN>" \
  -d '{
    "drone_id":"DRN-001",
    "kind":"status",
    "metric":"docking_status",
    "payload":{"is_online":false}
  }'
```

**2) WS Subscribe**
```json
{"type":"subscribe","drones":["DRN-001"]}
```

**3) WS Receive**
```json
{
  "drone_id": "DRN-001",
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
