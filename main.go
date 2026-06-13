package main

import (
	"fmt"
	"sync"
)

type Gordan struct {
	mu   sync.RWMutex
	data map[string]string
}

func NewGordan() *Gordan {
	return &Gordan{
		data: make(map[string]string),
	}
}

func main() {
	g := NewGordan()
	fmt.Println(g)
}
