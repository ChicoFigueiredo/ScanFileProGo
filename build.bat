@echo off
setlocal
echo =========================================================
echo        Compilando ScanFile Pro (Windows Native Engine)   
echo =========================================================

taskkill /F /IM scanfile.exe >nul 2>&1
timeout /t 1 /nobreak >nul
if exist scanfile.exe del /f /q scanfile.exe >nul 2>&1

echo [*] Executando go vet...
go vet ./...
if %ERRORLEVEL% neq 0 (
    echo [X] Erro no go vet!
    pause
    exit /b %ERRORLEVEL%
)

echo [*] Executando testes unitarios...
go test ./...
if %ERRORLEVEL% neq 0 (
    echo [X] Erro nos testes unitarios!
    pause
    exit /b %ERRORLEVEL%
)

echo [*] Compilando scanfile.exe...
go build -ldflags="-s -w" -o scanfile.exe .
if %ERRORLEVEL% neq 0 (
    echo [X] Erro ao compilar scanfile.exe!
    pause
    exit /b %ERRORLEVEL%
)

echo =========================================================
echo [+] Compilacao concluida com sucesso! (scanfile.exe)
echo =========================================================
pause
