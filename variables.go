package main

import (
	"os"
)

var SERVERKEY = os.Getenv("server_key")
var MAX_ACCOUNT_LIFE_MINS = 10