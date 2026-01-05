@echo off
setlocal enabledelayedexpansion

:: UI Build Script for Windows
:: Usage: build.bat [command]
:: Commands: setup, build, deps, monaco, run, clean, clean-all, help

if "%1"=="" goto help
if "%1"=="setup" goto setup
if "%1"=="build" goto build
if "%1"=="deps" goto deps
if "%1"=="monaco" goto monaco
if "%1"=="run" goto run
if "%1"=="clean" goto clean
if "%1"=="clean-all" goto clean-all
if "%1"=="help" goto help
goto help

:setup
echo === Full Setup ===
call :deps
call :monaco
call :build
goto end

:build
echo === Building UI server ===
call :monaco
go build -o ui.exe .
if %errorlevel% neq 0 (
    echo Build failed!
    exit /b 1
)
echo Build complete: ui.exe
goto end

:deps
echo === Installing npm dependencies ===
npm install monaco-editor@0.45.0
if %errorlevel% neq 0 (
    echo npm install failed!
    exit /b 1
)
goto end

:monaco
if not exist "node_modules\monaco-editor" (
    echo Monaco not found, installing dependencies...
    call :deps
)
echo === Setting up Monaco Editor ===
if not exist "static\monaco" mkdir static\monaco
xcopy /E /Y /Q node_modules\monaco-editor\min\vs static\monaco\vs\
echo Monaco Editor installed to static\monaco\
goto end

:run
if not exist "ui.exe" (
    echo ui.exe not found, building...
    call :build
)
echo === Running UI server ===
ui.exe -dir ..\examples\hello
goto end

:clean
echo === Cleaning build artifacts ===
if exist "ui.exe" del ui.exe
if exist "static\monaco" rmdir /S /Q static\monaco
echo Clean complete
goto end

:clean-all
call :clean
echo === Deep cleaning ===
if exist "node_modules" rmdir /S /Q node_modules
if exist "package.json" del package.json
if exist "package-lock.json" del package-lock.json
echo Deep clean complete
goto end

:help
echo UI Build Script for Windows
echo.
echo Usage: build.bat [command]
echo.
echo Commands:
echo   setup      Full setup (deps + monaco + build)
echo   build      Build the UI server (ui.exe)
echo   deps       Install npm dependencies
echo   monaco     Copy Monaco to static directory
echo   run        Build and run with ..\examples\hello
echo   clean      Remove build artifacts
echo   clean-all  Remove all generated files
echo   help       Show this help
goto end

:end
endlocal