FROM golang:alpine AS build
WORKDIR /src/runsd
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 go install ./runsd

FROM scratch
COPY --from=build /go/bin/runsd /runsd
