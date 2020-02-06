<p align="center"><img height="150px" src="https://github.com/maxisme/transferme.it/raw/master/public_html/images/og_logo.png"></p>

# [transferme.it](https://transferme.it/)

## [Mac App](https://github.com/maxisme/transfermeit) | [Website](https://github.com/maxisme/transferme.it) | Backend


[![Build Status](https://github.com/maxisme/transfermeit-backend/workflows/Transfer%20Me%20It/badge.svg)](https://github.com/maxisme/transfermeit-backend/actions)

To install migrate:
```
$ curl -L https://packagecloud.io/mattes/migrate/gpgkey | apt-key add -
$ echo "deb https://packagecloud.io/mattes/migrate/ubuntu/ xenial main" > /etc/apt/sources.list.d/migrate.list
$ apt-get update
$ apt-get install -y migrate
```

To initialise schema first create a database `notifi` then run:
```
migrate -database mysql://root:@/notifi up
```

To create new migrations run:
```
$ migrate create -ext sql -dir sql/ -seq remove_col
```