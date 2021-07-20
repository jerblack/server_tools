package main

import (
	"database/sql"
	"fmt"
	_ "github.com/go-sql-driver/mysql"
	"os"
	"strings"
)

var dbUser, dbPass string

const (
	userFile = "/run/secrets/db_user"
	pwFile   = "/run/secrets/db_pw"
	userEnv  = "DB_USER"
	pwEnv    = "DB_PW"
	dbServer = "db.home"
	dbDb     = "db"
	dbPort   = "3306"
)

func NewDb() *PlayedDb {
	db := PlayedDb{}
	db.getUserPass()
	db.setDSN()
	db.createTable()
	return &db
}

type PlayedDb struct {
	user, pass, dsn string
}

func (pdb *PlayedDb) getUserPass() {
	pdb.user = os.Getenv(userEnv)
	pdb.pass = os.Getenv(pwEnv)
	if pdb.user != "" && pdb.pass != "" {
		return
	}

	if !fileExists(userFile) || !fileExists(pwFile) {
		p("pdb user or pass secrets not found")
		os.Exit(1)
	}

	b, e := os.ReadFile(userFile)
	chkFatal(e)
	pdb.user = strings.Trim(string(b), "\n")
	b, e = os.ReadFile(pwFile)
	chkFatal(e)
	pdb.pass = strings.Trim(string(b), "\n")

}
func (pdb *PlayedDb) setDSN() {
	pdb.dsn = fmt.Sprintf("%s:%s@tcp(%s:%s)/%s", pdb.user, pdb.pass, dbServer, dbPort, dbDb)
}
func (pdb *PlayedDb) createTable() {
	db, e := sql.Open("mysql", pdb.dsn)
	chkFatal(e)
	defer db.Close()

	_, e = db.Exec(`create table if not exists cast_played (file varchar(1024) not null,playlist varchar(255) not null);`)
	chk(e)
}
func (pdb *PlayedDb) addWatched(pl *Playlist, file string) {
	db, e := sql.Open("mysql", pdb.dsn)
	chkFatal(e)
	defer db.Close()
	_, e = db.Exec(`insert into cast_played (file, playlist) values(?, ?);`, file, pl.Id)
	chk(e)
}
func (pdb *PlayedDb) clearWatched(pl *Playlist) {
	db, e := sql.Open("mysql", pdb.dsn)
	chkFatal(e)
	defer db.Close()
	_, e = db.Exec(`delete from cast_played where playlist = ?;`, pl.Id)
	chk(e)
}
func (pdb *PlayedDb) getWatched(pl *Playlist) []string {
	db, e := sql.Open("mysql", pdb.dsn)
	chkFatal(e)
	defer db.Close()

	rows, e := db.Query(`select file from cast_played where playlist = ?;`, pl.Id)
	chkFatal(e)
	var files []string
	for rows.Next() {
		var file string
		e = rows.Scan(&file)
		chkFatal(e)
		files = append(files, file)
	}
	return files
}
