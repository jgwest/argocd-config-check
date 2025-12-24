package main

import (
	"fmt"
	"os"
	"strings"

	corev1 "k8s.io/api/core/v1"
)

func failWithError(str string, err error) {
	if err != nil {
		fmt.Println("Error:", str, err)
	} else {
		fmt.Println("Error:", str)
	}

	os.Exit(1)
}

// containerArgsContainsParam returns true if args contains --paramKey=value or --paramKey value, false otherwise.
func containerArgsContainsParamKV(args []string, paramKey string, paramValue string) bool {

	// If calling function specified '--paramKey' (rather than only 'paramKey') then just strip it.
	paramKey = strings.TrimPrefix(paramKey, "--")

	for i, arg := range args {
		// Strip quotes from the argument. It's technically valid to include these in an arg string, but we don't care about them here.
		arg = strings.ReplaceAll(arg, "'", "")
		arg = strings.ReplaceAll(arg, "\"", "")

		// Case 1: --paramKey=paramValue
		if arg == "--"+paramKey+"="+paramValue {
			return true
		}

		// Case 2: --paramKey followed by paramValue as next argument
		if arg == "--"+paramKey && i+1 < len(args) {
			nextArg := args[i+1]
			nextArg = strings.ReplaceAll(nextArg, "'", "")
			nextArg = strings.ReplaceAll(nextArg, "\"", "")
			if nextArg == paramValue {
				return true
			}
		}
	}

	return false
}

// containerArgsContainsParam returns true if args contains --paramKey=(any value) or --paramKey (any value)
// - This function can be used when you only care about the prescence of a param, not its value
func containerArgsContainsParam(args []string, paramKey string) bool {

	// If calling function specified '--paramKey' (rather than only 'paramKey') then just strip it.
	paramKey = strings.TrimPrefix(paramKey, "--")

	for i, arg := range args {
		// Strip quotes from the argument. It's technically valid to include these in an arg string, but we don't care about them here.
		arg = strings.ReplaceAll(arg, "'", "")
		arg = strings.ReplaceAll(arg, "\"", "")

		// Case 1: --paramKey=anyValue
		if strings.HasPrefix(arg, "--"+paramKey+"=") {
			return true
		}

		// Case 2: --paramKey followed by any value as next argument
		if arg == "--"+paramKey && i+1 < len(args) {
			return true
		}
	}

	return false
}

// TODO: change to Key
func containerEnvVarContainsName(envs []corev1.EnvVar, name string) bool {
	for _, envVar := range envs {
		if envVar.Name == name {
			return true
		}
	}

	return false
}

func containerEnvVarContainsKeyValue(envs []corev1.EnvVar, key string, value string) bool {
	for _, envVar := range envs {
		if envVar.Name == key && envVar.Value == value {
			return true
		}
	}

	return false
}
