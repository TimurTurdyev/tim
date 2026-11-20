// Package bindif: сокет, привязанный к сетевому интерфейсу, - соединение идёт мимо туннеля, маршруты не меняются.
package bindif

import "errors"

// ErrUnsupported: на этой системе сокет к интерфейсу без root не привязать.
var ErrUnsupported = errors.New("привязка сокета к интерфейсу есть только на macOS")
