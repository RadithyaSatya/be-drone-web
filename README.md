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
