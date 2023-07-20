FROM golang:1.20-alpine AS build

WORKDIR /app

COPY go.mod go.sum ./
RUN go mod download

COPY *.go ./
RUN go vet -v
RUN go test -v
RUN CGO_ENABLED=0 go build -o /apsc

FROM gcr.io/distroless/static:nonroot

COPY --from=build /apsc /
CMD ["/apsc"]
