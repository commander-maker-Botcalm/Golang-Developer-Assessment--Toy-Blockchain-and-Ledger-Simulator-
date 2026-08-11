# three-node-demo.ps1
# Run this from the toy-blockchain folder in PowerShell.
# It starts three node processes in new PowerShell windows and configures them as peers.

$root = Split-Path -Parent $MyInvocation.MyCommand.Definition
Set-Location $root

function Start-Node {
    param(
        [string]$listen,
        [string[]]$peers,
        [string]$file,
        [string]$windowTitle
    )

    $peerFlags = $peers | ForEach-Object { "--peer $_" } | Out-String
    $peerFlags = $peerFlags -replace "\s+", ' '
    $command = "cd `"$root`"; go run ./cmd run-node --listen $listen --file $file $peerFlags"

    Start-Process powershell -ArgumentList '-NoExit','-Command',$command -WindowStyle Normal -WorkingDirectory $root
    Write-Host "Started $windowTitle on $listen using $file"
}

# Initialize a fresh shared chain state for node1/node2/node3.
if (-Not (Test-Path "$root\node1.json")) {
    go run ./cmd init --file node1.json --difficulty 1
}
Copy-Item -Force "$root\node1.json" "$root\node2.json"
Copy-Item -Force "$root\node1.json" "$root\node3.json"

Start-Node -listen ":8081" -peers @('http://localhost:8082','http://localhost:8083') -file "node1.json" -windowTitle 'Node A'
Start-Node -listen ":8082" -peers @('http://localhost:8081','http://localhost:8083') -file "node2.json" -windowTitle 'Node B'
Start-Node -listen ":8083" -peers @('http://localhost:8081','http://localhost:8082') -file "node3.json" -windowTitle 'Node C'

Write-Host "\nThree-node demo started. Use node1/node2/node3 endpoints on ports 8081, 8082, 8083."
Write-Host "Example: go run ./cmd addtx SYSTEM Alice 100 --file node1.json" 
Write-Host "Then: go run ./cmd mine --file node1.json --peer http://localhost:8082 --peer http://localhost:8083"
