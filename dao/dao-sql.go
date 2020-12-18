package dao

import (
	"database/sql"
	"encoding/json"
	"github.com/golang/protobuf/jsonpb"
	"github.com/omecodes/bome"
	"github.com/omecodes/libome"
	"sync"
)

type appsMapCursor struct {
	sync.Mutex
	bome.Cursor
	next            *ome.Application
	err             error
	filters         []ApplicationFilter
	parsingRequired bool
}

func (a *appsMapCursor) HasNext() bool {
	a.Lock()
	defer a.Unlock()

	if a.err != nil {
		return false
	}

	a.parseNext()
	return a.next != nil
}

func (a *appsMapCursor) Next() (*ome.Application, error) {
	a.Lock()
	defer a.Unlock()

	if a.err != nil {
		return nil, a.err
	}

	if a.parsingRequired {
		a.parseNext()
	}
	a.parsingRequired = false

	app := a.next
	err := a.err

	a.next = nil
	a.err = nil

	return app, err
}

func (a *appsMapCursor) Close() error {
	a.Lock()
	defer a.Unlock()

	return a.Cursor.Close()
}

func (a *appsMapCursor) parseNext() {
	if a.err != nil {
		return
	}

	for a.Cursor.HasNext() {
		passed := true
		var o interface{}
		o, a.err = a.Cursor.Next()
		if a.err != nil {
			return
		}
		a.next = o.(*ome.Application)

		for _, filter := range a.filters {
			passed = filter(a.next)
			if !passed {
				break
			}
		}

		if passed {
			return
		}

		a.next = nil
	}
}

type appsDictCursor struct {
	sync.Mutex
	bome.Cursor
	next            *ome.Application
	err             error
	filters         []ApplicationFilter
	parsingRequired bool
}

func (a *appsDictCursor) HasNext() bool {
	a.Lock()
	defer a.Unlock()

	if a.err != nil {
		return false
	}

	a.parseNext()
	return a.next != nil
}

func (a *appsDictCursor) Next() (*ome.Application, error) {
	a.Lock()
	defer a.Unlock()

	if a.err != nil {
		return nil, a.err
	}

	if a.parsingRequired {
		a.parseNext()
	}
	a.parsingRequired = false

	app := a.next
	err := a.err

	a.next = nil
	a.err = nil

	return app, err
}

func (a *appsDictCursor) Close() error {
	a.Lock()
	defer a.Unlock()
	return a.Cursor.Close()
}

func (a *appsDictCursor) parseNext() {
	if a.err != nil {
		return
	}

	for a.Cursor.HasNext() {
		passed := true

		var o interface{}
		o, a.err = a.Cursor.Next()
		if a.err != nil {
			return
		}

		entry := o.(*bome.MapEntry)
		app := new(ome.Application)

		a.err = jsonpb.UnmarshalString(entry.Value, app)
		if a.err != nil {
			return
		}
		a.next = app

		for _, filter := range a.filters {
			passed = filter(a.next)
			if !passed {
				break
			}
		}

		if passed {
			return
		}

		a.next = nil
	}
}

type ApplicationsDB interface {
	SaveApplication(application *ome.Application) error
	GetApplication(applicationID string) (*ome.Application, error)
	ListApplicationForUser(user string, filters ...ApplicationFilter) (AppCursor, error)
	ListAllApplications(filters ...ApplicationFilter) (AppCursor, error)
	DeleteApplication(applicationID string) error
}

type AppCursor interface {
	HasNext() bool
	Next() (*ome.Application, error)
	Close() error
}

type sqlApplicationsDB struct {
	userApps *bome.JSONMap
}

func (s *sqlApplicationsDB) ListAllApplications(filters ...ApplicationFilter) (AppCursor, error) {
	cursor, err := s.userApps.List()
	if err != nil {
		return nil, err
	}
	return &appsDictCursor{Cursor: cursor, filters: filters}, nil
}

func (s *sqlApplicationsDB) SaveApplication(application *ome.Application) error {
	encoded, err := json.Marshal(application)
	if err != nil {
		return err
	}
	entry := &bome.MapEntry{
		Key:   application.Id,
		Value: string(encoded),
	}
	return s.userApps.Save(entry)
}

func (s *sqlApplicationsDB) GetApplication(applicationID string) (*ome.Application, error) {
	value, err := s.userApps.Get(applicationID)
	if err != nil {
		return nil, err
	}
	a := &ome.Application{}
	err = json.Unmarshal([]byte(value), a)
	return a, err
}

func (s *sqlApplicationsDB) ListApplicationForUser(user string, filters ...ApplicationFilter) (AppCursor, error) {
	cursor, err := s.userApps.Search(bome.JsonAtEq("$.info.created_by", bome.StringExpr(user)), bome.StringScanner)
	if err != nil {
		return nil, err
	}
	return &appsMapCursor{
		Cursor:          cursor,
		filters:         filters,
		parsingRequired: false,
	}, nil
}

func (s *sqlApplicationsDB) DeleteApplication(applicationID string) error {
	return s.userApps.Delete(applicationID)
}

func NewSQLApplicationsDB(db *sql.DB, dialect string, tableName string) (ApplicationsDB, error) {
	dao := new(sqlApplicationsDB)
	apps, err := bome.NewJSONMap(db, dialect, "applications")
	if err != nil {
		return nil, err
	}
	dao.userApps = apps
	return dao, nil
}

type ApplicationFilter func(*ome.Application) bool
