package base_db

import (
	"database/sql"
	. "github.com/jerblack/server_tools/base"
	_ "github.com/mattn/go-sqlite3"
)

func DqQuery(query string, dbFile string, params ...interface{}) [][]interface{} {
	db, err := sql.Open("sqlite3", dbFile)
	ChkFatal(err)
	defer db.Close()
	rows, e := db.Query(query, params...)
	Chk(e)
	defer rows.Close()
	cols, e := rows.Columns()
	Chk(e)
	var results [][]interface{}
	n := len(cols)
	for rows.Next() {
		resultPtrs := make([]interface{}, n)
		result := make([]interface{}, n)
		for i := 0; i < n; i++ {
			var iface interface{}
			resultPtrs[i] = &iface
		}
		err = rows.Scan(resultPtrs...)
		Chk(err)
		for i := 0; i < n; i++ {
			result[i] = *(resultPtrs[i].(*interface{}))
		}
		results = append(results, result)
	}
	return results
}

func DbExec(query string, dbFile string, params ...interface{}) {
	db, err := sql.Open("sqlite3", dbFile)
	ChkFatal(err)
	defer db.Close()
	_, err = db.Exec(query, params...)
	Chk(err)
}
