# Build script for ScanFile Pro on Windows (PowerShell)
$ErrorActionPreference = "Stop"

Write-Host "=========================================================" -ForegroundColor Cyan
Write-Host "       Compilando ScanFile Pro (Windows Native Engine)   " -ForegroundColor Cyan
Write-Host "=========================================================" -ForegroundColor Cyan

# 1. Check if scanfile.exe is currently running and terminate if needed
$running = Get-Process -Name "scanfile" -ErrorAction SilentlyContinue
if ($running) {
    Write-Host "[!] Detectado scanfile.exe em execucao (PID: $($running.Id))." -ForegroundColor Yellow
    Write-Host "[*] Encerrando processo para liberar gravacao do binario..." -ForegroundColor Yellow
    Stop-Process -Name "scanfile" -Force -ErrorAction SilentlyContinue
    Start-Sleep -Milliseconds 600
}

Remove-Item -Force .\scanfile.exe -ErrorAction SilentlyContinue

# 2. Code Verification
Write-Host "[1/3] Executando analise estatica de codigo (go vet)..." -ForegroundColor Gray
go vet ./...
if ($LASTEXITCODE -ne 0) {
    Write-Host "[X] Falha na verificacao do go vet!" -ForegroundColor Red
    exit $LASTEXITCODE
}

# 3. Unit Testing
Write-Host "[2/3] Executando testes unitarios automatizados..." -ForegroundColor Gray
go test ./...
if ($LASTEXITCODE -ne 0) {
    Write-Host "[X] Falha nos testes automatizados!" -ForegroundColor Red
    exit $LASTEXITCODE
}

# 4. Compilation with high-performance flags (-s -w strips debug symbols for speed & compact size)
Write-Host "[3/3] Gerando executavel nativo (scanfile.exe)..." -ForegroundColor Green
go build -ldflags="-s -w" -o scanfile.exe .
if ($LASTEXITCODE -ne 0) {
    Write-Host "[X] Erro durante a compilacao!" -ForegroundColor Red
    exit $LASTEXITCODE
}

$fileSize = (Get-Item ".\scanfile.exe").Length / 1MB
Write-Host "=========================================================" -ForegroundColor Green
Write-Host ("[+] Compilacao concluida com sucesso! -> scanfile.exe ({0:N2} MB)" -f $fileSize) -ForegroundColor Green
Write-Host "=========================================================" -ForegroundColor Green
