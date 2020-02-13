<p align="center"><img height="150px" src="https://github.com/maxisme/transferme.it/raw/master/public_html/images/og_logo.png"></p>

# [transferme.it](https://transferme.it/)

## [Mac App](https://github.com/maxisme/transfermeit) | [Website](https://github.com/maxisme/transferme.it) | Backend


[![Build Status](https://github.com/maxisme/transfermeit-backend/workflows/Transfer%20Me%20It/badge.svg)](https://github.com/maxisme/transfermeit-backend/actions)

[![Build Status](https://github.com/maxisme/transfermeit-backend/workflows/notifi/badge.svg)](https://github.com/maxisme/transfermeit-backend/actions)
[![Coverage Status](https://codecov.io/gh/maxisme/transfermeit-backend/branch/master/graph/badge.svg)](https://codecov.io/gh/maxisme/transfermeit-backend)
[![Supported Go Versions](https://img.shields.io/badge/go-1.12%20|%201.13%20|%201.14-green&style=plastic)](https://github.com/maxisme/transfermeit-backend/actions)
[![Go Report Card](https://goreportcard.com/badge/github.com/maxisme/transfermeit-backend)](https://goreportcard.com/report/github.com/maxisme/transfermeit-backend)
To install migrate:
```
$ curl -L https://packagecloud.io/mattes/migrate/gpgkey | apt-key add -
$ echo "deb https://packagecloud.io/mattes/migrate/ubuntu/ xenial main" > /etc/apt/sources.list.d/migrate.list
$ apt-get update
$ apt-get install -y migrate
```

To initialise schema first create a database `transfermeit` then run:
```
migrate -database mysql://root:@/transfermeit up
```

To create new migrations run:
```
$ migrate create -ext sql -dir sql/ -seq remove_col
```