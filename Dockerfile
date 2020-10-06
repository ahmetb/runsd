FROM golang:alpine
WORKDIR /src/runsd
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN go install ./sample-app
RUN go install ./runsd

FROM alpine
RUN apk add --no-cache bind-tools curl
COPY --from=0 /go/bin/runsd /bin/runsd
COPY --from=0 /go/bin/sample-app /sample_app
ENTRYPOINT ["runsd", "-v=5", "--", "/sample_app"]
