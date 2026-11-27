; MagicDown Windows Per-User GUI Installer (Inno Setup 6)
; Compiles to dist\MagicDown-Setup-v<AppVersion>.exe
; PrivilegesRequired=lowest installs into %LOCALAPPDATA%\Programs\MagicDown with no UAC prompt.

#ifndef AppVersion
  #define AppVersion "0.1.2"
#endif

#define AppName "MagicDown"
#define AppPublisher "HongDucDevLabs"
#define AppURL "https://github.com/Hong-Duc-Dev-Labs/magic-down"
#define AppExeName "magicdown.exe"
#define HostExeName "magicdown-host.exe"

[Setup]
AppId={{8F6A42B1-9B19-4E8B-9F2A-7C64E8A31D20}
AppName={#AppName}
AppVersion={#AppVersion}
AppVerName={#AppName} v{#AppVersion}
AppPublisher={#AppPublisher}
AppPublisherURL={#AppURL}
AppSupportURL={#AppURL}/issues
AppUpdatesURL={#AppURL}/releases
DefaultDirName={localappdata}\Programs\{#AppName}
DefaultGroupName={#AppName}
DisableProgramGroupPage=yes
PrivilegesRequired=lowest
OutputDir=..\dist
OutputBaseFilename=MagicDown-Setup-v{#AppVersion}
SetupIconFile=..\cmd\magicdown\magicdown.ico
UninstallDisplayIcon={app}\{#AppExeName}
Compression=lzma2/ultra64
SolidCompression=yes
WizardStyle=modern
ArchitecturesAllowed=x64compatible
ArchitecturesInstallIn64BitMode=x64compatible
CloseApplications=yes
RestartApplications=no

[Languages]
Name: "english"; MessagesFile: "compiler:Default.isl"

[Tasks]
Name: "desktopicon"; Description: "{cm:CreateDesktopIcon}"; GroupDescription: "{cm:AdditionalIcons}"; Flags: unchecked
Name: "autostart"; Description: "Launch MagicDown minimized to System Tray with Windows"; GroupDescription: "Startup:"; Flags: unchecked

[Files]
Source: "..\dist\magicdown.exe"; DestDir: "{app}"; Flags: ignoreversion
Source: "..\dist\magicdown-host.exe"; DestDir: "{app}"; Flags: ignoreversion
Source: "..\dist\magicdown.ico"; DestDir: "{app}"; Flags: ignoreversion skipifsourcedoesntexist
Source: "..\dist\config\*"; DestDir: "{app}\config"; Flags: ignoreversion recursesubdirs createallsubdirs skipifsourcedoesntexist
Source: "..\dist\extension\*"; DestDir: "{app}\extension"; Flags: ignoreversion recursesubdirs createallsubdirs skipifsourcedoesntexist
Source: "install-host.ps1"; DestDir: "{app}\scripts"; Flags: ignoreversion
Source: "uninstall-host.ps1"; DestDir: "{app}\scripts"; Flags: ignoreversion

[Icons]
Name: "{autoprograms}\{#AppName}"; Filename: "{app}\{#AppExeName}"; IconFilename: "{app}\magicdown.ico"
Name: "{autodesktop}\{#AppName}"; Filename: "{app}\{#AppExeName}"; IconFilename: "{app}\magicdown.ico"; Tasks: desktopicon

[Registry]
Root: HKCU; Subkey: "Software\Microsoft\Windows\CurrentVersion\Run"; ValueType: string; ValueName: "MagicDown"; ValueData: """{app}\{#AppExeName}"" --minimized"; Flags: uninsdeletevalue; Tasks: autostart

[Run]
Filename: "powershell.exe"; Parameters: "-NoProfile -ExecutionPolicy Bypass -WindowStyle Hidden -File ""{app}\scripts\install-host.ps1"" -HostExe ""{app}\{#HostExeName}"""; Flags: runhidden waituntilterminated; StatusMsg: "Registering browser Native Messaging Host..."
Filename: "{app}\{#AppExeName}"; Description: "{cm:LaunchProgram,{#StringChange(AppName, '&', '&&')}}"; Flags: nowait postinstall skipifsilent

[UninstallRun]
Filename: "powershell.exe"; Parameters: "-NoProfile -ExecutionPolicy Bypass -WindowStyle Hidden -File ""{app}\scripts\uninstall-host.ps1"""; Flags: runhidden waituntilterminated; RunOnceId: "MagicDownUninstallNativeHost"
