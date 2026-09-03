@echo off
setlocal
cd /d "%~dp0"

echo =========================================================
echo        Compilando ScanFile Pro (Windows Native Engine)
echo =========================================================

rem Libera o binario de saida. NAO encerra processos: um scanfile.exe de outro
rem diretorio pode estar em uso. Se o arquivo local estiver travado, avisamos.
if exist scanfile.exe del /f /q scanfile.exe >nul 2>&1
if exist scanfile.exe goto :locked

echo [1/4] Executando go vet...
go vet ./...
if %ERRORLEVEL% neq 0 goto :fail_vet

echo [2/4] Executando testes automatizados...
go test ./...
if %ERRORLEVEL% neq 0 goto :fail_test

echo [3/4] Executando testes da interface...
where node >nul 2>&1
if %ERRORLEVEL% neq 0 goto :skip_ui
if not exist "ui\tests" goto :skip_ui
rem Aspas obrigatorias: o Node recente nao expande diretorio em --test, e
rem quem resolve o curinga e o proprio Node, nao o cmd.
node --test "ui/tests/**/*.test.mjs"
if %ERRORLEVEL% neq 0 goto :fail_ui
goto :build

:skip_ui
echo     (ignorado: node ou ui\tests ausente)

:build
echo [4/4] Compilando scanfile.exe...
go build -ldflags="-s -w" -o scanfile.exe .
if %ERRORLEVEL% neq 0 goto :fail_build

echo =========================================================
echo [+] Compilacao concluida com sucesso! (scanfile.exe)
echo =========================================================
pause
exit /b 0

:locked
echo [X] Nao foi possivel remover scanfile.exe (arquivo em uso).
echo     Feche a instancia iniciada a partir deste diretorio e rode de novo.
pause
exit /b 1

:fail_vet
echo [X] Erro no go vet!
pause
exit /b 1

:fail_test
echo [X] Erro nos testes automatizados!
pause
exit /b 1

:fail_ui
echo [X] Erro nos testes da interface!
pause
exit /b 1

:fail_build
echo [X] Erro ao compilar scanfile.exe!
pause
exit /b 1
