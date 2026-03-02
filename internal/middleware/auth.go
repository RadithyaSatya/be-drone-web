package middleware

import (
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"net/http"
	"os"
	"strings"
	"time"

	"xflight-backend/internal/auth"

	"github.com/golang-jwt/jwt/v5"
)

func AuthMiddleware(db *sql.DB) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			deviceToken := strings.TrimSpace(r.Header.Get("X-Device-Token"))
			if deviceToken != "" {
				if db == nil {
					http.Error(w, "database not configured", http.StatusInternalServerError)
					return
				}

				pepper := strings.TrimSpace(os.Getenv("DEVICE_TOKEN_PEPPER"))
				if pepper == "" {
					http.Error(w, "DEVICE_TOKEN_PEPPER not set", http.StatusInternalServerError)
					return
				}

				tokenHash := hashDeviceToken(deviceToken, pepper)

				var tokenID int64
				var scopeType string
				var uavID sql.NullInt32
				var dockingID sql.NullInt32

				err := db.QueryRow(`
					SELECT id, scope_type, uav_id, docking_id
					FROM device_tokens
					WHERE token_hash = $1
					  AND revoked_at IS NULL`,
					tokenHash,
				).Scan(&tokenID, &scopeType, &uavID, &dockingID)
				if err == sql.ErrNoRows {
					http.Error(w, "unauthorized", http.StatusUnauthorized)
					return
				}
				if err != nil {
					http.Error(w, "database error", http.StatusInternalServerError)
					return
				}

				_, _ = db.Exec(`UPDATE device_tokens SET last_used_at = $2 WHERE id = $1`, tokenID, time.Now().UTC())

				claims := &jwt.RegisteredClaims{Subject: "device"}
				ctx := auth.WithClaims(r.Context(), claims)

				deviceClaims := &auth.DeviceClaims{
					TokenID:   tokenID,
					ScopeType: scopeType,
				}
				if uavID.Valid {
					value := int(uavID.Int32)
					deviceClaims.UavID = &value
				}
				if dockingID.Valid {
					value := int(dockingID.Int32)
					deviceClaims.DockingID = &value
				}
				ctx = auth.WithDeviceClaims(ctx, deviceClaims)

				next.ServeHTTP(w, r.WithContext(ctx))
				return
			}

			secret := os.Getenv("JWT_SECRET")
			if secret == "" {
				http.Error(w, "JWT_SECRET not set", http.StatusInternalServerError)
				return
			}

			bearer, err := auth.ParseBearerToken(r.Header.Get("Authorization"))
			if err != nil {
				http.Error(w, "unauthorized", http.StatusUnauthorized)
				return
			}

			claims := &jwt.RegisteredClaims{}
			if err := auth.ParseAndValidateJWT([]byte(secret), bearer, claims); err != nil {
				http.Error(w, "unauthorized", http.StatusUnauthorized)
				return
			}

			ctx := auth.WithClaims(r.Context(), claims)
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

func hashDeviceToken(token, pepper string) string {
	sum := sha256.Sum256([]byte(token + pepper))
	return hex.EncodeToString(sum[:])
}
