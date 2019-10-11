FROM golang:1.12-alpine

USER root

ENV GOROOT /usr/local/go
ENV GOPATH $HOME/go
ENV PATH $PATH:$GOROOT/bin

RUN apk update
RUN apk upgrade
RUN apk add git
RUN apk add gcc
RUN apk add libc-dev

RUN mkdir /app
ADD . /app/
WORKDIR /app
RUN chmod 777 -R /go/
RUN go build -o transfermeit .
CMD ["/app/transfermeit"]