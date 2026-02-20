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
- `metric` (required when `kind=telemetry`)
- `payload` (required): object

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
  "drone_id": "DRN-001",
  "kind": "status",
  "ts": "2026-02-05T10:01:00Z",
  "payload": {
    "online": false,
    "state": "IDLE"
  }
}
```

`metric` is only used for `kind=telemetry` and represents the telemetry channel name (`battery`, `location`, `docking`, `imu`, etc.).

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

**1) Send telemetry via HTTP**
```bash
curl -X POST http://127.0.0.1:8080/realtime/telemetry \
  -H "Content-Type: application/json" \
  -H "X-Device-Token: <DEVICE_TOKEN>" \
  -d '{
    "drone_id":"DRN-001",
    "kind":"telemetry",
    "metric":"docking",
    "payload":{"online":false}
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
  "kind": "telemetry",
  "metric": "docking",
  "ts": "2026-02-04T06:25:48.885371Z",
  "payload": {
    "online": true
  }
}
```

### Mission History
- `POST /missions/{id}/start` will insert a record into `mission_history` with status `InProgress`.
- `POST /mission-history/{history_id}/complete` will update that history row to `Completed`.
- `GET /missions/{id}/history` returns the mission details + its run history.
- `GET /mission-history` returns all history entries (paginated).

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

List all history (paginated):
```
GET /mission-history?page=1&limit=20
```
Notes:
- Default `page=1`, `limit=20`
- Max `limit=100`

Fields:
- `started_at`: time when `start` was called.
- `completed_at`: time when `complete` was called (null while `InProgress`).
- `media`: list of uploaded media (image/video) for each history entry.

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
DEVICE_TOKEN=change-me
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
