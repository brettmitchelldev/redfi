package redfi

import "fmt"

type Logger = func(int, string)

func MakeLogger(level int) Logger {
	return func(msgLevel int, msg string) {
		if level >= msgLevel {
			fmt.Printf(msg)
		}
	}
}
