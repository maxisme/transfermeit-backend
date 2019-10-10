FROM golang:1.12-alpine

ENV GOROOT /usr/local/go
ENV GOPATH $HOME/go
ENV PATH $PATH:$GOROOT/bin

RUN mkdir /.cache && chmod 777 /.cache
RUN apk update
RUN apk upgrade
RUN apk add git
RUN apk add gcc
RUN apk add libc-dev

RUN mkdir /app
ADD . /app/
WORKDIR /app
RUN go build -o transfermeit .
CMD ["/app/transfermeit"]