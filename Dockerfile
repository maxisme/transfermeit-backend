FROM golang:1.12-alpine

ADD . /app/
WORKDIR /app
RUN go build -o transfermeit .
CMD ["/app/transfermeit"]