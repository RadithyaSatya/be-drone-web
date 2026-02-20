package server

type Config struct {
	DBConn string
	Addr   string
}

func LoadConfig() Config {
	return Config{
		DBConn: getEnv("DB_CONN", "user=macbook dbname=xflight sslmode=disable password="),
		Addr:   getEnv("ADDR", "127.0.0.1:8080"),
	}
}
