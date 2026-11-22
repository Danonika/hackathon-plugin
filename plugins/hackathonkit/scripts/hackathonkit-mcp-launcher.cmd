@echo off
setlocal EnableExtensions DisableDelayedExpansion
node "%~dp0hackathonkit-mcp-launcher.js" %*
exit /b %ERRORLEVEL%
