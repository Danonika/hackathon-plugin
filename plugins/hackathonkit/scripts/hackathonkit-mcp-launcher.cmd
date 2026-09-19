@echo off
setlocal EnableExtensions
set "SCRIPT_DIR=%~dp0"
for %%I in ("%SCRIPT_DIR%..\..\..") do set "REPO_ROOT=%%~fI"
if not exist "%REPO_ROOT%\docker-compose.yml" (
  echo Hackathon Agent Kit launcher: docker-compose.yml not found at "%REPO_ROOT%" 1>&2
  exit /b 1
)
if exist "%REPO_ROOT%\bin\hackathonkit-mcp.exe" (
  "%REPO_ROOT%\bin\hackathonkit-mcp.exe" --repo-root "%REPO_ROOT%"
  exit /b %ERRORLEVEL%
)
pushd "%REPO_ROOT%"
docker compose -f "%REPO_ROOT%\docker-compose.yml" run --rm -T hackathonkit-mcp
set "EXIT_CODE=%ERRORLEVEL%"
popd
exit /b %EXIT_CODE%
