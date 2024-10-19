// Package shellq: экранирование аргументов для sh. Одно место, чтобы показанная команда
// и выполненная команда не расходились.
package shellq

import "strings"

// safe: символы, которые sh не трактует особым образом. Всё остальное требует кавычек.
const safe = "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789-_./=:@,+%"

// Quote берёт аргумент в одинарные кавычки всегда. Внутри них особых символов нет,
// кроме самой кавычки: её закрываем, вставляем экранированную и открываем снова.
func Quote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}

// Join собирает командную строку, беря в кавычки только то, что без них изменило бы смысл.
func Join(args []string) string {
	out := make([]string, len(args))
	for i, a := range args {
		out[i] = a
		if a == "" || strings.ContainsFunc(a, func(r rune) bool { return !strings.ContainsRune(safe, r) }) {
			out[i] = Quote(a)
		}
	}
	return strings.Join(out, " ")
}
