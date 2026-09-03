# Build script for ScanFile Pro on Windows (PowerShell)
#
#   .\build.ps1          -> go vet + go test ./... + testes da UI + build
#   .\build.ps1 -Race    -> usa "go test -race ./..." (exige CGO e um compilador C)
[CmdletBinding()]
param(
    [switch]$Race
)

$ErrorActionPreference = "Stop"

$RepoRoot = if ($PSScriptRoot) { $PSScriptRoot } else { (Get-Location).Path }
$ExePath = Join-Path $RepoRoot "scanfile.exe"
$UiTestsDir = Join-Path $RepoRoot "ui\tests"

function Invoke-Step {
    param(
        [string]$Label,
        [scriptblock]$Action
    )
    Write-Host $Label -ForegroundColor Gray
    & $Action
    if ($LASTEXITCODE -ne 0) {
        Write-Host "[X] Falha em: $Label" -ForegroundColor Red
        exit $LASTEXITCODE
    }
}

Write-Host "=========================================================" -ForegroundColor Cyan
Write-Host "       Compilando ScanFile Pro (Windows Native Engine)   " -ForegroundColor Cyan
Write-Host "=========================================================" -ForegroundColor Cyan

Push-Location $RepoRoot
try {
    # 1. Libera o binario de saida. Encerra SOMENTE o scanfile.exe deste
    #    diretorio; outras instancias na maquina nao sao tocadas.
    if (Test-Path -LiteralPath $ExePath) {
        $ownProcesses = @(
            Get-Process -Name "scanfile" -ErrorAction SilentlyContinue | Where-Object {
                $procPath = $null
                try { $procPath = $_.Path } catch { $procPath = $null }
                # Sem caminho legivel (processo elevado ou de outro usuario) => nao e nosso.
                $procPath -and ($procPath -ieq $ExePath)
            }
        )
        foreach ($proc in $ownProcesses) {
            Write-Host "[!] Encerrando scanfile.exe deste diretorio (PID: $($proc.Id))..." -ForegroundColor Yellow
            Stop-Process -Id $proc.Id -Force -ErrorAction SilentlyContinue
        }
        if ($ownProcesses.Count -gt 0) {
            Start-Sleep -Milliseconds 600
        }

        Remove-Item -LiteralPath $ExePath -Force -ErrorAction SilentlyContinue
        if (Test-Path -LiteralPath $ExePath) {
            Write-Host "[X] Nao foi possivel remover $ExePath (arquivo em uso)." -ForegroundColor Red
            Write-Host "    Feche a instancia iniciada a partir deste diretorio e rode de novo." -ForegroundColor Red
            exit 1
        }
    }

    # 2. Analise estatica
    Invoke-Step "[1/4] Executando analise estatica de codigo (go vet)..." { go vet ./... }

    # 3. Testes automatizados
    if ($Race) {
        Invoke-Step "[2/4] Executando testes automatizados com detector de corrida (-race)..." { go test -race ./... }
    } else {
        Invoke-Step "[2/4] Executando testes automatizados..." { go test ./... }
    }

    # 4. Testes da interface (opcionais: so se node e ui/tests existirem)
    #    O padrao entre aspas e obrigatorio: o Node 24 no Windows nao expande um
    #    diretorio em --test (trata o caminho como arquivo de teste e falha), e o
    #    PowerShell nao expande curingas para comandos nativos -- quem faz o glob
    #    e o proprio Node.
    if ((Get-Command node -ErrorAction SilentlyContinue) -and (Test-Path -LiteralPath $UiTestsDir)) {
        Invoke-Step "[3/4] Executando testes da interface (node --test ui/tests)..." {
            node --test "ui/tests/**/*.test.mjs"
        }
    } else {
        Write-Host "[3/4] Testes da interface ignorados (node ou ui/tests ausente)." -ForegroundColor DarkGray
    }

    # 5. Compilacao (-s -w remove simbolos de depuracao: binario menor)
    Write-Host "[4/4] Gerando executavel nativo (scanfile.exe)..." -ForegroundColor Green
    go build -ldflags="-s -w" -o scanfile.exe .
    if ($LASTEXITCODE -ne 0) {
        Write-Host "[X] Erro durante a compilacao!" -ForegroundColor Red
        exit $LASTEXITCODE
    }

    $fileSize = (Get-Item -LiteralPath $ExePath).Length / 1MB
    Write-Host "=========================================================" -ForegroundColor Green
    Write-Host ("[+] Compilacao concluida com sucesso! -> scanfile.exe ({0:N2} MB)" -f $fileSize) -ForegroundColor Green
    Write-Host "=========================================================" -ForegroundColor Green
}
finally {
    Pop-Location
}
