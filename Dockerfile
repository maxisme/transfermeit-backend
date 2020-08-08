FROM golang:1.14 as builder

WORKDIR /
ADD . .
RUN CGO_ENABLED=0 GOOS=linux go build -a -installsuffix cgo -ldflags '-extldflags "-static"' -o /transfermeit .

FROM scratch
COPY --from=builder /etc/ssl/certs/ca-certificates.crt /etc/ssl/certs/ca-certificates.crt
COPY --from=builder /transfermeit /transfermeit
WORKDIR /
ENTRYPOINT ["/transfermeit"]
