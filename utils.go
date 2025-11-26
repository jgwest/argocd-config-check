package main

import (
	"fmt"
	"os"
)

func failWithError(str string, err error) {
	if err != nil {
		fmt.Println("Error:", str, err)
	} else {
		fmt.Println("Error:", str)
	}

	os.Exit(1)
}
