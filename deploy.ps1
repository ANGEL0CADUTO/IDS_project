# ==============================================================================
# Script PowerShell Semplificato per la gestione del Progetto IDS
# ==============================================================================

# --- CONFIGURAZIONE ---
# Modifica queste variabili con i tuoi dati
$KeyPath = "C:\Users\aroma\.ssh\ids-project-key.pem"
$Ec2Ip = "3.85.86.100"
$Ec2User = "ubuntu"
# --------------------


# --- FUNZIONI HELPER ---
function Run-LocalCommand { param([string]$Command)
    Write-Host "--- Esecuzione Locale: $Command ---" -ForegroundColor Green
    Invoke-Expression $Command
}
function Run-RemoteCommand { param([string]$Command)
    Write-Host "--- Esecuzione su AWS ($Ec2Ip): $Command ---" -ForegroundColor Cyan
    ssh -i $KeyPath -t "${Ec2User}@${Ec2Ip}" $Command
}


# --- LOGICA PRINCIPALE ---
# Leggiamo il primo argomento passato allo script
$Action = $args[0]
# Leggiamo il secondo argomento (opzionale)
$Service = $args[1]

if (-not $Action) {
    $Action = "help"
}

Write-Host "`nAzione richiesta: $Action" -ForegroundColor Yellow

if ($Action -eq "help") {
    Write-Host "Uso: ./deploy.ps1 [azione] [servizio_opzionale]"
    Write-Host "`n--- Comandi Locali ---"
    Write-Host "  up, down, logs, test, test-benign, test-malicious, clean-all"
    Write-Host "`n--- Comandi Remoti (AWS) ---"
    Write-Host "  aws-setup, aws-deploy, aws-up, aws-down, aws-logs, aws-clean-all"
    Write-Host "  aws-test-benign, aws-test-malicious"
}

# --- Comandi Locali ---
if ($Action -eq "build")          { Run-LocalCommand "docker compose build --no-cache" }
if ($Action -eq "up")             { Run-LocalCommand "docker compose up -d" }
if ($Action -eq "down")           { Run-LocalCommand "docker compose down" }
if ($Action -eq "logs")           { Run-LocalCommand "docker compose logs -f" }
if ($Action -eq "test")           { Run-LocalCommand "go test -v -count=1 ./..." }
if ($Action -eq "test-benign")    { Run-LocalCommand "go run ./cmd/test-client/main.go -mode=benign -addr=localhost:50051" }
if ($Action -eq "test-malicious") { Run-LocalCommand "go run ./cmd/test-client/main.go -mode=malicious -addr=localhost:50051" }
if ($Action -eq "clean-all")      { Run-LocalCommand "docker compose down -v" }
if ($Action -eq "aws-clean-influx") {
    Run-RemoteCommand -Command "cd IDS_project && make clean-influx"
}
# --- Comandi Remoti (AWS) ---
if ($Action -eq "aws-setup") {
    $script = "sudo apt-get update -y && sudo apt-get install -y curl git make; sudo apt-get remove docker docker-engine docker.io containerd runc -y; curl -fsSL https://get.docker.com | sudo sh; sudo usermod -aG docker $Ec2User; sudo apt-get install -y docker-compose-plugin; echo '====== SETUP COMPLETATO SU AWS ======'; echo 'Per favore, esci e riconnettiti manualmente UNA VOLTA per applicare i permessi di Docker.'"
    Run-RemoteCommand -Command $script
}
if ($Action -eq "aws-deploy") {
    $script = "if [ -d 'IDS_project' ]; then cd IDS_project && git pull; else git clone https://github.com/ANGEL0CADUTO/IDS_project.git && cd IDS_project; fi; make down && make up"
    Run-RemoteCommand -Command $script
}
if ($Action -eq "aws-up") {
    Run-RemoteCommand -Command "cd IDS_project && docker compose up -d $Service"
}
if ($Action -eq "aws-down") {
    Run-RemoteCommand -Command "cd IDS_project && docker compose stop $Service"
}
if ($Action -eq "aws-logs") {
    Run-RemoteCommand -Command "cd IDS_project && make logs"
}
if ($Action -eq "aws-clean-all") {
    Run-RemoteCommand -Command "cd IDS_project && make clean-all"
}
if ($Action -eq "aws-test-benign") {
    Run-LocalCommand "go run ./cmd/test-client/main.go -mode=benign -addr=${Ec2Ip}:50051"
}
if ($Action -eq "aws-test-malicious") {
    Run-LocalCommand "go run ./cmd/test-client/main.go -mode=malicious -addr=${Ec2Ip}:50051"
}