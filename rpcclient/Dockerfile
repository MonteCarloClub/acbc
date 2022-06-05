FROM golang:1.18.1-bullseye

WORKDIR $GOPATH/src/github.com/MonteCarloClub/acbc_rpc_client

COPY ../go.mod ./
COPY ../go.sum ./
RUN go mod download

COPY .. ./

RUN ls -la /

RUN  go build -o /acbc

EXPOSE 8080

CMD [ "/rpcclient/examples/acbccorehttp/main"]