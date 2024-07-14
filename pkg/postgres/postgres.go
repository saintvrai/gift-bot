package postgres

import (
	"fmt"
	"gift-bot/pkg/config"
	"github.com/golang-migrate/migrate"
	"github.com/golang-migrate/migrate/database/postgres"
	_ "github.com/golang-migrate/migrate/source/file"
	_ "github.com/jackc/pgx"
	"github.com/jmoiron/sqlx"
	"github.com/sirupsen/logrus"
)

const (
	UserTable = "users"
)

// New provides db connection and checks it with Ping()
func New() (*sqlx.DB, error) {
	dbUrl := fmt.Sprintf("host=%s port=%s user=%s dbname=%s password=%s sslmode=%s",
		config.GlobalСonfig.DB.Host, config.GlobalСonfig.DB.Port, config.GlobalСonfig.DB.Username, config.GlobalСonfig.DB.Name,
		config.GlobalСonfig.DB.Password, config.GlobalСonfig.DB.SSL)
	db, err := sqlx.Open("postgres", dbUrl)
	if err != nil {
		return nil, err
	}

	err = db.Ping()
	if err != nil {
		return nil, err
	}

	return db, nil
}

func MigrateDB(db *sqlx.DB, dbname string) {
	driver, err := postgres.WithInstance(db.DB, &postgres.Config{})
	if err != nil {
		logrus.Fatalf("couldn't get database instance for running migrations; %s", err.Error())
	}

	m, err := migrate.NewWithDatabaseInstance(fmt.Sprintf("file://%s", config.GlobalСonfig.DB.Migrations), dbname, driver)
	if err != nil {
		logrus.Fatalf("couldn't create migrate instance; %s", err.Error())
	}

	if err := m.Up(); err != nil {
		if err.Error() == "no change" {
			//logrus.Infof("database migration doesn't required, no changes")
		} else {
			logrus.Fatalf("couldn't run database migrations; %s", err.Error())
		}
	} else {
		logrus.Info("database migration was run successfully")
	}
}
