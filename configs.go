package empire

import (
	"database/sql"
	"database/sql/driver"

	"github.com/lib/pq/hstore"
)

// ConfigID represents a unique identifier for a Config.
type ConfigID string

// Scan implements the sql.Scanner interface.
func (id *ConfigID) Scan(src interface{}) error {
	if src, ok := src.([]byte); ok {
		*id = ConfigID(src)
	}

	return nil
}

// Value implements the driver.Value interface.
func (id ConfigID) Value() (driver.Value, error) {
	return driver.Value(string(id)), nil
}

// Config represents a collection of environment variables.
type Config struct {
	ID      ConfigID `json:"id" db:"id"`
	Vars    Vars     `json:"vars" db:"vars"`
	AppName AppName  `json:"-" db:"app_id"`
}

// NewConfig initializes a new config based on the old config, with the new
// variables provided.
func NewConfig(old *Config, vars Vars) *Config {
	v := mergeVars(old.Vars, vars)

	return &Config{
		AppName: old.AppName,
		Vars:    v,
	}
}

// Variable represents the name of an environment variable.
type Variable string

// Vars represents a variable -> value mapping.
type Vars map[Variable]string

// Scan implements the sql.Scanner interface.
func (v *Vars) Scan(src interface{}) error {
	h := hstore.Hstore{}
	if err := h.Scan(src); err != nil {
		return err
	}

	*v = hstoreToVars(h)

	return nil
}

// Value implements the driver.Value interface.
func (v Vars) Value() (driver.Value, error) {
	h := varsToHstore(v)
	return h.Value()
}

// ConfigsRepository represents an interface for retrieving and storing Config's.
type ConfigsRepository interface {
	// Head returns the current Config for the app.
	Head(AppName) (*Config, error)

	// Find returns the specific version of a Config for an app.
	Find(ConfigID) (*Config, error)

	// Store stores the Config for the app.
	Push(*Config) (*Config, error)
}

func NewConfigsRepository(db DB) (ConfigsRepository, error) {
	return &configsRepository{db}, nil
}

// configsRepository is an implementation of the ConfigsRepository interface backed by
// a DB.
type configsRepository struct {
	DB
}

// Head implements Repository Head.
func (r *configsRepository) Head(appName AppName) (*Config, error) {
	return r.findBy("app_id", string(appName))
}

// Find implements Repository Find.
func (r *configsRepository) Find(id ConfigID) (*Config, error) {
	return r.findBy("id", string(id))
}

// Push implements Repository Push.
func (r *configsRepository) Push(config *Config) (*Config, error) {
	return CreateConfig(r.DB, config)
}

func (r *configsRepository) findBy(field string, v interface{}) (*Config, error) {
	var config Config

	if err := r.DB.SelectOne(&config, `select id, app_id, vars from configs where `+field+` = $1 order by created_at desc limit 1`, v); err != nil {
		if err == sql.ErrNoRows {
			return nil, nil
		}

		return nil, err
	}

	return &config, nil
}

// CreateConfig inserts a Config in the database.
func CreateConfig(db Inserter, config *Config) (*Config, error) {
	return config, db.Insert(config)
}

func hstoreToVars(h hstore.Hstore) Vars {
	vars := make(Vars)

	for k, v := range h.Map {
		vars[Variable(k)] = v.String
	}

	return vars
}

func varsToHstore(vars Vars) hstore.Hstore {
	m := make(map[string]sql.NullString)

	for k, v := range vars {
		m[string(k)] = sql.NullString{
			Valid:  true,
			String: v,
		}
	}

	return hstore.Hstore{
		Map: m,
	}
}

// mergeVars copies all of the vars from a, and merges b into them, returning a
// new Vars.
func mergeVars(old, new Vars) Vars {
	vars := make(Vars)

	for n, v := range old {
		vars[n] = v
	}

	for n, v := range new {
		if v != "" {
			vars[n] = v
		} else {
			delete(vars, n)
		}
	}

	return vars
}

// ConfigsService represents a service for interacting with Configs.
type ConfigsService interface {
	// Apply applies the vars to the apps latest Config.
	Apply(*App, Vars) (*Config, error)

	// Returns the Head Config for an App.
	Head(*App) (*Config, error)
}

// configsService is a base implementation of the ConfigsService.
type configsService struct {
	Repository ConfigsRepository
}

// NewConfigsService returns a new Service instance.
func NewConfigsService(r ConfigsRepository) (ConfigsService, error) {
	return &configsService{
		Repository: r,
	}, nil
}

// Apply merges the provided Vars into the latest Config and returns a new
// Config.
func (s *configsService) Apply(app *App, vars Vars) (*Config, error) {
	l, err := s.Repository.Head(app.Name)
	if err != nil {
		return nil, err
	}

	// If the app doesn't have a config, just build a new one.
	if l == nil {
		l = &Config{}
	}

	l.AppName = app.Name

	c := NewConfig(l, vars)

	return s.Repository.Push(c)
}

// Gets the config for an app. If the app doesn't have a config, it will create
// a new one.
func (s *configsService) Head(app *App) (*Config, error) {
	c, err := s.Repository.Head(app.Name)
	if err != nil {
		return nil, err
	}

	if c == nil {
		return s.Repository.Push(&Config{
			AppName: app.Name,
			Vars:    make(Vars),
		})
	}

	return c, nil
}
