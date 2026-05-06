FROM golang:latest AS build

WORKDIR /app

COPY go.mod go.sum ./
RUN go mod download

COPY api/ api/
COPY internal/ internal/
COPY cmd/ cmd/

RUN go test ./...
RUN CGO_ENABLED=0 GOOS=linux \
    go build \
    -ldflags="-w -s" \
    -installsuffix 'static' \
    -o ./specialist-booking-webapi-srv ./cmd/specialist-booking-api-service

FROM scratch

LABEL org.opencontainers.image.authors="Tomáš Drga, Matej Drienovský"
LABEL org.opencontainers.image.title="Specialist Booking WebAPI Service"
LABEL org.opencontainers.image.description="WEBAPI for specialist appointment and time slot management"

ENV SPECIALIST_BOOKING_API_ENVIRONMENT=production
ENV SPECIALIST_BOOKING_API_PORT=8080
ENV SPECIALIST_BOOKING_API_MONGODB_HOST=mongodb
ENV SPECIALIST_BOOKING_API_MONGODB_PORT=27017
ENV SPECIALIST_BOOKING_API_MONGODB_DATABASE=specialist-booking
ENV SPECIALIST_BOOKING_API_MONGODB_COLLECTION=clinics
ENV SPECIALIST_BOOKING_API_MONGODB_USERNAME=root
ENV SPECIALIST_BOOKING_API_MONGODB_PASSWORD=
ENV SPECIALIST_BOOKING_API_MONGODB_TIMEOUT_SECONDS=5

COPY --from=build /app/specialist-booking-webapi-srv ./

EXPOSE 8080
ENTRYPOINT ["./specialist-booking-webapi-srv"]
