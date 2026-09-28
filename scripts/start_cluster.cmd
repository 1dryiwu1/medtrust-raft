@echo off
setlocal
powershell -ExecutionPolicy Bypass -File "%~dp0start_cluster.ps1" %*
endlocal
