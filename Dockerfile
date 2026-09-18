# ========================================
# 1. Build Stage
# ========================================
FROM golang:bookworm AS builder
WORKDIR /app

# ติดตั้ง Dependencies สำหรับ Build (ต้องใช้ -dev เพื่อ Compile CGO)
RUN apt-get update && apt-get install -y \
    libtesseract-dev \
    libleptonica-dev

# จัดการ Go Modules (Cache เลเยอร์นี้ไว้เพื่อความเร็วในการ Build ครั้งต่อไป)
COPY go.mod go.sum ./
RUN go mod download

# คัดลอกโค้ดและ Build
COPY . .
RUN CGO_ENABLED=1 GOOS=linux go build -o go-ocr .

# ========================================
# 2. Production Stage
# ========================================
FROM debian:bookworm-slim
WORKDIR /app

# ใช้ --no-install-recommends เพื่อลดขนาด Image ไม่ให้บวม
# ใช้ libtesseract5 แทน -dev เพราะตอนรันต้องการแค่ Shared Library (.so)
RUN apt-get update && apt-get install -y --no-install-recommends \
    poppler-utils \
    tesseract-ocr \
    tesseract-ocr-tha \
    tesseract-ocr-eng \
    libtesseract5 \
    ca-certificates \
    tzdata \
    && apt-get clean \
    && rm -rf /var/lib/apt/lists/*

# คัดลอกไฟล์ที่ Build เสร็จแล้วจาก Builder
COPY --from=builder /app/go-ocr .

# ตั้งค่า Environment & Port
ENV PORT=8080
ENV GIN_MODE=release
EXPOSE 8080

# คำสั่งรัน
CMD ["./go-ocr"]