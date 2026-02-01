FROM golang:1.25-alpine AS build
WORKDIR /app
COPY . .
RUN go mod download
RUN CGO_ENABLED=0 go build -o go-apcups-nis

FROM alpine:3.22
COPY --from=build /app/go-apcups-nis /
EXPOSE 25
CMD ["/go-apcups-nis"]
