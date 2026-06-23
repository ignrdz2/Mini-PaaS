# stage build: compilar el binario del orquestador
FROM golang:1.25-alpine AS build

WORKDIR /src

# descargar dependencias primero para aprovechar la caché de capas
COPY go.mod go.sum ./
RUN go mod download

# compilar el binario estático
COPY . .
RUN CGO_ENABLED=0 go build -o /orchestrator ./cmd/orchestrator

# stage final: imagen mínima con git para que el orquestador pueda clonar repos
FROM golang:1.25-alpine

RUN apk add --no-cache git

COPY --from=build /orchestrator /orchestrator

EXPOSE 8080

ENTRYPOINT ["/orchestrator"]
