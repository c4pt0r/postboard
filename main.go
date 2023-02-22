//  Usage:
//  pb set key value
//  echo val | pb set key
//  pb get key
//  pb get key*

package main

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"path/filepath"

	"github.com/gookit/gcli/v3"

	_ "github.com/go-sql-driver/mysql"
)

var db *sql.DB
var configFilePath string

func init() {
	if os.Getenv("POSTBOARD_CONFIG") != "" {
		configFilePath = os.Getenv("POSTBOARD_CONFIG")
	} else {
		// default config is at $HOME/.postboard/config.json
		configFilePath = filepath.Join(os.Getenv("HOME"), ".postboard", "config.json")
	}
}

type Config struct {
	DSN string `json:"DSN"`
}

func readConfigFromStdin() (*Config, error) {
	var DSNInputed string
	fmt.Println("Please enter your database connection string:")
	fmt.Scanln(&DSNInputed)
	return &Config{
		DSN: DSNInputed,
	}, nil
}

func saveConfigToFile(config *Config, configFilePath string) error {
	// create directory
	os.MkdirAll(filepath.Dir(configFilePath), 0755)
	// create config file
	f, err := os.Create(configFilePath)
	if err != nil {
		return err
	}
	json.NewEncoder(f).Encode(config)
	f.Close()
	return nil
}

func loadConfig(configFilePath string) (*Config, error) {
	// default config is at $HOME/.postboard/config.json
	// if config file is not specified, load default config
	if _, err := os.Stat(configFilePath); os.IsNotExist(err) {
		// ask user for config
		config, err := readConfigFromStdin()
		if err != nil {
			return nil, err
		}
		// save config
		if err := saveConfigToFile(config, configFilePath); err != nil {
			return nil, err
		}
		return config, nil
	} else {
		// load config
		f, err := os.Open(configFilePath)
		if err != nil {
			return nil, err
		}
		var config Config
		json.NewDecoder(f).Decode(&config)
		f.Close()
		return &config, nil
	}
}

func prepareDatabase() error {
	var createTblStmt = `
CREATE TABLE IF NOT EXISTS postboard_kvs (
  k VARCHAR(255) NOT NULL,
  v BLOB NOT NULL,
  created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
  PRIMARY KEY (k)
);`
	_, err := db.Exec(createTblStmt)
	return err
}

func putKeyValue(key string, value []byte) error {
	var insertStmt = `INSERT INTO postboard_kvs (k, v) VALUES (?, ?) ON DUPLICATE KEY UPDATE v = VALUES(v);`
	_, err := db.Exec(insertStmt, key, value)
	return err
}

func getKey(key string) ([]byte, error) {
	var selectStmt = `SELECT v FROM postboard_kvs WHERE k = ?;`
	var value []byte
	err := db.QueryRow(selectStmt, key).Scan(&value)
	return value, err
}

func listKeysWithPrefix(prefix string) ([]string, error) {
	rows, err := db.Query("SELECT k FROM postboard_kvs WHERE k LIKE ? LIMIT 1000", prefix+"%")
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var keys []string
	for rows.Next() {
		var key string
		if err := rows.Scan(&key); err != nil {
			return nil, err
		}
		keys = append(keys, key)
	}
	return keys, nil
}

func main() {
	app := gcli.NewApp()
	app.Name = "pb"
	app.Desc = "postboard: A CLI application to manage configurations remotely"

	cfg, err := loadConfig(configFilePath)
	if err != nil {
		log.Fatal(err)
	}
	db, err = sql.Open("mysql", cfg.DSN)
	if err != nil {
		log.Fatal(err)
	}

	app.Add(&gcli.Command{
		Name: "config",
		Desc: "Set up the application configuration",
		Func: func(c *gcli.Command, args []string) error {
			// ask user for config
			config, err := readConfigFromStdin()
			if err != nil {
				return err
			}
			// save config
			if err := saveConfigToFile(config, configFilePath); err != nil {
				return err
			}
			return nil
		},
	})

	app.Add(&gcli.Command{
		Name: "set",
		Desc: "Set a configuration value",
		Config: func(c *gcli.Command) {
			c.AddArg("key", "The key of the configuration", true)
			c.AddArg("value", "The value of the configuration", false)
		},
		Func: func(c *gcli.Command, args []string) error {
			if c.Arg("key").String() == "" {
				return fmt.Errorf("key is empty")
			}
			var value string
			if c.Arg("value").String() == "" {
				// read from stdin
				fmt.Scanln(&value)
			} else {
				value = c.Arg("value").String()
			}
			return putKeyValue(c.Arg("key").String(), []byte(value))
		},
	})

	keysOnly := false
	app.Add(&gcli.Command{
		Name: "get",
		Desc: "Get a configuration value",
		Config: func(c *gcli.Command) {
			c.AddArg("key", "The key of the configuration", true)
			c.BoolOpt(&keysOnly, "k", "", true, "Only print keys")
		},
		Func: func(c *gcli.Command, args []string) error {
			key := c.Arg("key").String()
			if key == "" {
				return fmt.Errorf("key is empty")
			}
			if key[len(key)-1] == '*' {
				keys, err := listKeysWithPrefix(key[:len(key)-1])
				if err != nil {
					return err
				}
				for _, key := range keys {
					if keysOnly {
						fmt.Println(key)
					} else {
						val, err := getKey(key)
						if err != nil {
							return err
						}
						fmt.Printf("%s=%s\n", key, string(val))
					}
				}
			} else {
				val, err := getKey(key)
				if err != nil {
					return err
				}
				fmt.Println(string(val))
			}
			return nil
		},
	})
	app.Add(&gcli.Command{
		Name: "del",
		Desc: "Delete a configuration value",
		Func: func(c *gcli.Command, args []string) error {
			if len(args) != 1 {
				fmt.Println("Invalid number of arguments. Usage: pb del [key]")
				return nil
			}
			key := args[0]
			fmt.Printf("Deleting configuration %s...\n", key)
			return nil
		},
	})
	app.Run(nil)
}
