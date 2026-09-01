# ========================================
# 1. Build Stage
# ========================================
FROM golang:bookworm AS builder
WORKDIR /app

# ติดตั้ง Dependencies สำหรับ Build
RUN apt-get update && apt-get install -y \
    libtesseract-dev \
    libleptonica-dev

# จัดการ Go Modules
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

# เอา --no-install-recommends ออก และเพิ่ม libleptonica-dev เพื่อให้ Shared Libraries ครบถ้วน
RUN apt-get update && apt-get install -y \
    poppler-utils \
    tesseract-ocr \
    tesseract-ocr-tha \
    tesseract-ocr-eng \
    libtesseract-dev \
    libleptonica-dev \
    && apt-get clean \
    && rm -rf /var/lib/apt/lists/*

# คัดลอกไฟล์ที่ Build เสร็จแล้ว
COPY --from=builder /app/go-ocr .

# ตั้งค่า Environment & Port
ENV PORT=8080
EXPOSE 8080

# คำสั่งรัน
CMD ["./go-ocr"]