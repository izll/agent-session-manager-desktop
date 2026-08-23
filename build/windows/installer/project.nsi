Unicode true

####
## Please note: Template replacements don't work in this file. They are provided with default defines like
## mentioned underneath.
## If the keyword is not defined, "wails_tools.nsh" will populate them with the values from ProjectInfo.
## If they are defined here, "wails_tools.nsh" will not touch them. This allows to use this project.nsi manually
## from outside of Wails for debugging and development of the installer.
##
## For development first make a wails nsis build to populate the "wails_tools.nsh":
## > wails build --target windows/amd64 --nsis
## Then you can call makensis on this file with specifying the path to your binary:
## For a AMD64 only installer:
## > makensis -DARG_WAILS_AMD64_BINARY=..\..\bin\app.exe
## For a ARM64 only installer:
## > makensis -DARG_WAILS_ARM64_BINARY=..\..\bin\app.exe
## For a installer with both architectures:
## > makensis -DARG_WAILS_AMD64_BINARY=..\..\bin\app-amd64.exe -DARG_WAILS_ARM64_BINARY=..\..\bin\app-arm64.exe
####
## The following information is taken from the ProjectInfo file, but they can be overwritten here.
####
## !define INFO_PROJECTNAME    "MyProject" # Default "{{.Name}}"
## !define INFO_COMPANYNAME    "MyCompany" # Default "{{.Info.CompanyName}}"
## !define INFO_PRODUCTNAME    "MyProduct" # Default "{{.Info.ProductName}}"
## !define INFO_PRODUCTVERSION "1.0.0"     # Default "{{.Info.ProductVersion}}"
## !define INFO_COPYRIGHT      "Copyright" # Default "{{.Info.Copyright}}"
###
## !define PRODUCT_EXECUTABLE  "Application.exe"      # Default "${INFO_PROJECTNAME}.exe"
## !define UNINST_KEY_NAME     "UninstKeyInRegistry"  # Default "${INFO_COMPANYNAME}${INFO_PRODUCTNAME}"
####
# Per-user install: no elevation to install, and none to update.
#
# This has to be defined BEFORE wails_tools.nsh, which issues the
# RequestExecutionLevel itself and defaults the constant to "admin" — and whose
# wails.setShellContext macro reads the same constant to decide between
# SetShellVarContext all and current. Setting RequestExecutionLevel further down
# instead left the constant at "admin", so the installer ran unelevated while
# aiming its shortcuts at the all-users Start menu, where it could not write:
# the shortcut was simply never created.
!define REQUEST_EXECUTION_LEVEL "user"

# Pinned, because the default is "${INFO_COMPANYNAME}${INFO_PRODUCTNAME}" and a
# company name was added after 0.9.11 shipped. Letting the key follow that would
# make an upgrade look for a registry entry no existing install has: it would
# not find the previous version, would install beside it, and leave two entries
# in Add/Remove Programs. The value below is what installs up to 0.9.11 wrote.
!define UNINST_KEY_NAME "asmgr-desktopAgent Session Manager"
####
## Include the wails tools
####
!include "wails_tools.nsh"

# For ${If}/${EndIf} in .onInit, used to detect a previous machine-wide install.
!include "LogicLib.nsh"

# The version information for this two must consist of 4 parts
VIProductVersion "${INFO_PRODUCTVERSION}.0"
VIFileVersion    "${INFO_PRODUCTVERSION}.0"

VIAddVersionKey "CompanyName"     "${INFO_COMPANYNAME}"
VIAddVersionKey "FileDescription" "${INFO_PRODUCTNAME} Installer"
VIAddVersionKey "ProductVersion"  "${INFO_PRODUCTVERSION}"
VIAddVersionKey "FileVersion"     "${INFO_PRODUCTVERSION}"
VIAddVersionKey "LegalCopyright"  "${INFO_COPYRIGHT}"
VIAddVersionKey "ProductName"     "${INFO_PRODUCTNAME}"

# Enable HiDPI support. https://nsis.sourceforge.io/Reference/ManifestDPIAware
ManifestDPIAware true

!include "MUI.nsh"

!define MUI_ICON "..\icon.ico"
!define MUI_UNICON "..\icon.ico"
# !define MUI_WELCOMEFINISHPAGE_BITMAP "resources\leftimage.bmp" #Include this to add a bitmap on the left side of the Welcome Page. Must be a size of 164x314
!define MUI_FINISHPAGE_NOAUTOCLOSE # Wait on the INSTFILES page so the user can take a look into the details of the installation steps
!define MUI_ABORTWARNING # This will warn the user if they exit from the installer.

!insertmacro MUI_PAGE_WELCOME # Welcome to the installer page.
# !insertmacro MUI_PAGE_LICENSE "resources\eula.txt" # Adds a EULA page to the installer
!insertmacro MUI_PAGE_DIRECTORY # In which folder install page.
!insertmacro MUI_PAGE_INSTFILES # Installing page.

# Offer to launch the app from the finish page, ticked by default — finishing an
# install and then hunting for the shortcut is a needless extra step.
#
# The launch goes through explorer.exe rather than starting the exe directly:
# the installer runs elevated, and a child process inherits that, so a direct
# call would leave the app running as administrator for the whole session.
# Handing the path to Explorer runs it as the logged-in user instead.
!define MUI_FINISHPAGE_RUN
!define MUI_FINISHPAGE_RUN_FUNCTION LaunchAsUser
!define MUI_FINISHPAGE_RUN_TEXT "Run ${INFO_PRODUCTNAME}"

!insertmacro MUI_PAGE_FINISH # Finished installation page.

!insertmacro MUI_UNPAGE_INSTFILES # Uinstalling page

!insertmacro MUI_LANGUAGE "English" # Set the Language of the installer

## The following two statements can be used to sign the installer and the uninstaller. The path to the binaries are provided in %1
#!uninstfinalize 'signtool --file "%1"'
#!finalize 'signtool --file "%1"'

# Where to find the runtime DLLs that ship beside the executable.
#
# CI stages a copy in build\dlls before the -nsis build, because that build
# rewrites build\bin and would otherwise leave the libraries behind. Falling
# back to build\bin keeps a plain local `wails build --nsis` working, where the
# DLLs are still sitting next to the exe.
#
# `wails build` offers no way to pass -D through to makensis, so this is defined
# here rather than on the command line.
!ifndef ARG_WAILS_DLL_DIR
    !define ARG_WAILS_DLL_DIR "..\..\dlls"
!endif

Name "${INFO_PRODUCTNAME}"
OutFile "..\..\bin\${INFO_PROJECTNAME}-${ARCH}-installer.exe" # Name of the installer's file.
# Installed per user, under LocalAppData, rather than into Program Files. This
# keeps ordinary use and manual upgrades free of administrator rights. The
# current flat EXE+DLL layout deliberately does not update itself in-process:
# there is no atomic multi-file replacement or pre-loader crash recovery on
# Windows. Automatic installation stays disabled until a stable launcher owns
# versioned payload activation.
#
# Per-user install is what Chrome, VS Code and Discord do, for this reason. It
# also means the installer needs no administrator rights at all.
#
# No vendor folder above the product: the Wails default is
# ${INFO_COMPANYNAME}\${INFO_PRODUCTNAME}, but with no company name set Wails
# falls back to the project name, and the path becomes asmgr-desktop\Agent
# Session Manager — the same name twice, once machine-readable and once not.
InstallDir "$LOCALAPPDATA\Programs\${INFO_PRODUCTNAME}"

# Reinstall over an existing copy instead of beside it. Without this an upgrade
# would install to the default path and leave the previous install orphaned —
# two copies, two shortcuts, and an uninstaller for the one you are not running.
# It also keeps a user's own choice of directory across upgrades.
InstallDirRegKey HKCU "${UNINST_KEY}" "InstallLocation"
ShowInstDetails show # This will always show the installation details.

Function .onInit
   !insertmacro wails.checkArchitecture
   # Shortcut and uninstall paths resolve per user; the sections do this too,
   # via wails.setShellContext, but .onInit runs before any of them.
   SetShellVarContext current

   # Offer to clear out a previous machine-wide install.
   #
   # Installs before this one went to Program Files and registered under HKLM.
   # This installer looks in HKCU, finds nothing, and installs to LocalAppData —
   # leaving the old copy behind with its own shortcuts and its own uninstaller,
   # so the Start menu offers two of the same app and the wrong one may launch.
   # Both registry views: a 32-bit installer writes under WOW6432Node, and
   # which one an earlier release used is not something to assume.
   SetRegView 64
   ReadRegStr $R0 HKLM "${UNINST_KEY}" "UninstallString"
   ${If} $R0 == ""
      SetRegView 32
      ReadRegStr $R0 HKLM "${UNINST_KEY}" "UninstallString"
      SetRegView 64
   ${EndIf}
   # Fall back to the uninstaller on disk. A registry entry can be missing
   # while the install is still there — a failed uninstall, a cleaner, or a
   # write that never happened — and the leftover copy is just as confusing.
   ${If} $R0 == ""
      IfFileExists "$PROGRAMFILES64\${INFO_PRODUCTNAME}\uninstall.exe" 0 +2
         StrCpy $R0 '"$PROGRAMFILES64\${INFO_PRODUCTNAME}\uninstall.exe"'
   ${EndIf}
   ${If} $R0 != ""
      MessageBox MB_YESNO|MB_ICONQUESTION "An older system-wide installation was found. Remove it first? This version installs for the current user only. Removing the old one needs administrator approval." /SD IDYES IDNO skip_old_uninstall
      # Waited for: an uninstaller still running would delete files underneath
      # the install that follows it.
      ExecWait '$R0 /S'
      skip_old_uninstall:
   ${EndIf}
FunctionEnd

# Launch the app without passing the installer's elevated token down to it.
#
# ShellExecute from an elevated installer would start the app as administrator,
# and it would stay that way for the whole session: config written to the wrong
# profile, and every agent it spawns running with rights it never needed.
# Explorer runs as the logged-in user, so asking it to open the path drops back
# to that user's privileges.
Function LaunchAsUser
    Exec '"$WINDIR\explorer.exe" "$INSTDIR\${PRODUCT_EXECUTABLE}"'
FunctionEnd

Section
    !insertmacro wails.setShellContext

    !insertmacro wails.webview2runtime

    SetOutPath $INSTDIR

    !insertmacro wails.files

    # The runtime DLLs the binary links against. wails.files installs the
    # executable and nothing else, and without these the app does not start at
    # all: portaudio backs the dictation feature, and the three MinGW libraries
    # are what a CGO build links against. They sit beside the exe in the release
    # archive for exactly this reason.
    #
    # Built with a wildcard rather than four File lines so a new dependency
    # cannot be silently left out of the installer.
    File /nonfatal "${ARG_WAILS_DLL_DIR}\*.dll"

    CreateShortcut "$SMPROGRAMS\${INFO_PRODUCTNAME}.lnk" "$INSTDIR\${PRODUCT_EXECUTABLE}"
    CreateShortCut "$DESKTOP\${INFO_PRODUCTNAME}.lnk" "$INSTDIR\${PRODUCT_EXECUTABLE}"

    !insertmacro wails.associateFiles
    !insertmacro wails.associateCustomProtocols

    !insertmacro wails.writeUninstaller

    # wails.writeUninstaller does not record where the app went, so InstallDirRegKey
    # above would read an empty value and every upgrade would fall back to the
    # default path — orphaning an install that lives anywhere else. Written here
    # rather than in wails_tools.nsh, which the Wails CLI regenerates.
    SetRegView 64
    WriteRegStr HKCU "${UNINST_KEY}" "InstallLocation" "$INSTDIR"
SectionEnd

Section "uninstall"
    !insertmacro wails.setShellContext

    RMDir /r "$AppData\${PRODUCT_EXECUTABLE}" # Remove the WebView2 DataPath

    RMDir /r $INSTDIR

    Delete "$SMPROGRAMS\${INFO_PRODUCTNAME}.lnk"
    Delete "$DESKTOP\${INFO_PRODUCTNAME}.lnk"

    !insertmacro wails.unassociateFiles
    !insertmacro wails.unassociateCustomProtocols

    !insertmacro wails.deleteUninstaller
SectionEnd
