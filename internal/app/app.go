// Package app: то, что корень tim передаёт разделам.
package app

import "github.com/TimurTurdyev/tim/internal/config"

// Env: окружение команды - конфиг пользователя. Корень заполняет его перед запуском команды,
// разделы получают указатель при сборке команд.
type Env struct {
	Cfg *config.Config
}

// Save записывает конфиг на диск.
func (e *Env) Save() error { return config.Save(e.Cfg) }
