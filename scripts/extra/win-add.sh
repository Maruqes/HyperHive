#!/usr/bin/env bash

# Script para adicionar drivers VirtIO a uma ISO do Windows (BIOS e UEFI)
# Autor: HyperHive
# Data: 2025

set -euo pipefail

RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
NC='\033[0m' # No Color

echo -e "${GREEN}=== Script de Integração de Drivers VirtIO em ISO Windows ===${NC}\n"

# ---------- cleanup e trap ----------

WORK_DIR=""
ISO_MOUNT=""
VIRTIO_MOUNT=""
ISO_EXTRACT=""
OUTPUT_DIR=""

cleanup() {
    echo -e "\n${YELLOW}A limpar ficheiros temporários...${NC}"

    if [ -n "${ISO_MOUNT:-}" ] && mountpoint -q "$ISO_MOUNT" 2>/dev/null; then
        umount "$ISO_MOUNT" 2>/dev/null || true
    fi

    if [ -n "${VIRTIO_MOUNT:-}" ] && mountpoint -q "$VIRTIO_MOUNT" 2>/dev/null; then
        umount "$VIRTIO_MOUNT" 2>/dev/null || true
    fi

    if [ -n "${WORK_DIR:-}" ] && [ -d "$WORK_DIR" ]; then
        rm -rf "$WORK_DIR"
    fi

    echo -e "${GREEN}Limpeza concluída${NC}"
}

trap cleanup EXIT

# ---------- root & deps ----------

if [ "$EUID" -ne 0 ]; then 
    echo -e "${RED}Por favor, execute como root (sudo)${NC}"
    exit 1
fi

echo -e "${YELLOW}A verificar dependências...${NC}"
DEPENDENCIES=("genisoimage" "mkisofs" "wget" "rsync" "isoinfo" "xorriso")
MISSING_DEPS=()

for dep in "${DEPENDENCIES[@]}"; do
    if ! command -v "$dep" &> /dev/null; then
        MISSING_DEPS+=("$dep")
    fi
done

if [ ${#MISSING_DEPS[@]} -ne 0 ]; then
    echo -e "${RED}Dependências em falta: ${MISSING_DEPS[*]}${NC}"
    echo -e "${YELLOW}A instalar dependências (Fedora)...${NC}"
    
    PACKAGES_TO_INSTALL=()
    for dep in "${MISSING_DEPS[@]}"; do
        case $dep in
            "genisoimage"|"mkisofs")
                PACKAGES_TO_INSTALL+=("genisoimage")
                ;;
            "isoinfo")
                PACKAGES_TO_INSTALL+=("genisoimage")
                ;;
            "xorriso")
                PACKAGES_TO_INSTALL+=("xorriso")
                ;;
            "wget")
                PACKAGES_TO_INSTALL+=("wget")
                ;;
            "rsync")
                PACKAGES_TO_INSTALL+=("rsync")
                ;;
        esac
    done

    if [ ${#PACKAGES_TO_INSTALL[@]} -ne 0 ]; then
        read -r -a PACKAGES_TO_INSTALL <<< "$(printf '%s\n' "${PACKAGES_TO_INSTALL[@]}" | sort -u)"
        dnf install -y --skip-unavailable "${PACKAGES_TO_INSTALL[@]}"
    fi
fi

# ---------- escolher ISO ----------

echo -e "\n${GREEN}Por favor, indique o caminho completo da ISO do Windows:${NC}"
read -e -p "Caminho da ISO: " ISO_PATH

if [ ! -f "$ISO_PATH" ]; then
    echo -e "${RED}Erro: Ficheiro não encontrado: $ISO_PATH${NC}"
    exit 1
fi

if [[ ! "$ISO_PATH" =~ \.iso$ ]]; then
    echo -e "${RED}Erro: O ficheiro não é uma ISO${NC}"
    exit 1
fi

echo -e "${GREEN}ISO encontrada: $ISO_PATH${NC}"

# Tenta preservar o mesmo label da ISO original para evitar falhas de boot em UEFI
ISO_LABEL="Windows_VirtIO"
if command -v isoinfo &> /dev/null; then
    ISO_LABEL_RAW="$(isoinfo -d -i "$ISO_PATH" 2>/dev/null | awk -F': ' '/Volume id/ {print $2; exit}')"
    if [ -n "$ISO_LABEL_RAW" ]; then
        ISO_LABEL="$ISO_LABEL_RAW"
    fi
fi

# ---------- dirs de trabalho ----------

WORK_BASE="${WORK_BASE:-/mnt/512SvMan/shared/virtio_iso_work}"
mkdir -p "$WORK_BASE"

WORK_DIR="$(mktemp -d "$WORK_BASE/virtio_iso_work_XXXXXX")"
ISO_MOUNT="$WORK_DIR/iso_mount"
ISO_EXTRACT="$WORK_DIR/iso_extract"
VIRTIO_DIR="$WORK_DIR/virtio"
OUTPUT_DIR="$WORK_DIR/output"
VIRTIO_MOUNT="$WORK_DIR/virtio_mount"

mkdir -p "$ISO_MOUNT" "$ISO_EXTRACT" "$VIRTIO_DIR" "$OUTPUT_DIR" "$VIRTIO_MOUNT"

echo -e "\n${YELLOW}Diretório de trabalho: $WORK_DIR${NC}"

# ---------- montar ISO original ----------

echo -e "${YELLOW}A montar ISO original...${NC}"
mount -o loop,ro "$ISO_PATH" "$ISO_MOUNT"

echo -e "${YELLOW}A extrair conteúdo da ISO... (isto pode demorar)${NC}"
rsync -av --progress "$ISO_MOUNT/" "$ISO_EXTRACT/"

umount "$ISO_MOUNT"
chmod -R u+w "$ISO_EXTRACT"

# ---------- download VirtIO ----------

echo -e "\n${YELLOW}A descarregar drivers VirtIO...${NC}"
VIRTIO_ISO_URL="https://fedorapeople.org/groups/virt/virtio-win/direct-downloads/stable-virtio/virtio-win.iso"
VIRTIO_ISO="$WORK_DIR/virtio-win.iso"

# segue redirects (301) automaticamente
wget -c "$VIRTIO_ISO_URL" -O "$VIRTIO_ISO"

if [ ! -f "$VIRTIO_ISO" ]; then
    echo -e "${RED}Erro ao descarregar drivers VirtIO${NC}"
    exit 1
fi

echo -e "${YELLOW}A montar ISO dos drivers VirtIO...${NC}"
mount -o loop,ro "$VIRTIO_ISO" "$VIRTIO_MOUNT"

# ---------- copiar drivers ----------

DRIVERS_DIR="$ISO_EXTRACT/drivers"
mkdir -p "$DRIVERS_DIR"

echo -e "${YELLOW}A copiar drivers VirtIO...${NC}"
for dir in NetKVM viostor vioscsi Balloon qemupciserial qxldod vioinput viorng viogpudo guest-agent; do
    if [ -d "$VIRTIO_MOUNT/$dir" ]; then
        cp -a "$VIRTIO_MOUNT/$dir" "$DRIVERS_DIR/"
    fi
done

umount "$VIRTIO_MOUNT"

# ---------- autounattend opcional ----------

echo -e "${YELLOW}A criar ficheiro de configuração para drivers (autounattend_drivers.xml)...${NC}"
cat > "$ISO_EXTRACT/autounattend_drivers.xml" << 'EOF'
<?xml version="1.0" encoding="utf-8"?>
<unattend xmlns="urn:schemas-microsoft-com:unattend">
    <settings pass="windowsPE">
        <component name="Microsoft-Windows-PnpCustomizationsWinPE" processorArchitecture="amd64" publicKeyToken="31bf3856ad364e35" language="neutral" versionScope="nonSxS" xmlns:wcm="http://schemas.microsoft.com/WMIConfig/2002/State">
            <DriverPaths>
                <PathAndCredentials wcm:action="add" wcm:keyValue="1">
                    <Path>d:\drivers\viostor</Path>
                </PathAndCredentials>
                <PathAndCredentials wcm:action="add" wcm:keyValue="2">
                    <Path>d:\drivers\NetKVM</Path>
                </PathAndCredentials>
                <PathAndCredentials wcm:action="add" wcm:keyValue="3">
                    <Path>d:\drivers\Balloon</Path>
                </PathAndCredentials>
                <PathAndCredentials wcm:action="add" wcm:keyValue="4">
                    <Path>d:\drivers\vioscsi</Path>
                </PathAndCredentials>
            </DriverPaths>
        </component>
    </settings>
</unattend>
EOF

# ---------- preparar criação da ISO ----------

ISO_NAME="$(basename "$ISO_PATH" .iso)"
NEW_ISO="$OUTPUT_DIR/${ISO_NAME}_virtio.iso"

BOOT_IMG="boot/etfsboot.com"
if [ ! -f "$ISO_EXTRACT/$BOOT_IMG" ]; then
    echo -e "${RED}Não encontrei ${BOOT_IMG} na ISO extraída. A ISO pode não ser uma ISO Windows válida.${NC}"
    exit 1
fi

# Imagem de boot UEFI (efisys.bin ou similar)
EFI_BOOT_IMG="$(find "$ISO_EXTRACT/efi" -maxdepth 3 -type f -iname 'efisys*.bin' 2>/dev/null | head -n1 || true)"
EFI_BOOT_REL=""

if [ -n "$EFI_BOOT_IMG" ]; then
    EFI_BOOT_REL="${EFI_BOOT_IMG#$ISO_EXTRACT/}"
    echo -e "${GREEN}Imagem UEFI encontrada: $EFI_BOOT_REL${NC}"
else
    echo -e "${YELLOW}Aviso: Imagem UEFI não encontrada. A ISO resultante poderá não arrancar em UEFI.${NC}"
fi

# escolher ferramenta para gerar ISO (preferir xorriso, mas só se suportar -udf)
MKISO_TOOL=""
MKISO_TOOL_NAME=""

if command -v xorriso &> /dev/null; then
    if xorriso -as mkisofs -udf --version >/dev/null 2>&1; then
        MKISO_TOOL="$(command -v xorriso) -as mkisofs"
        MKISO_TOOL_NAME="xorriso -as mkisofs"
    else
        echo -e "${YELLOW}Aviso: xorriso encontrado mas sem suporte para -udf. A usar outra ferramenta.${NC}"
    fi
fi

if [ -z "$MKISO_TOOL" ] && command -v genisoimage &> /dev/null; then
    MKISO_TOOL="$(command -v genisoimage)"
    MKISO_TOOL_NAME="genisoimage"
fi

if [ -z "$MKISO_TOOL" ] && command -v mkisofs &> /dev/null; then
    MKISO_TOOL="$(command -v mkisofs)"
    MKISO_TOOL_NAME="mkisofs"
fi

if [ -z "$MKISO_TOOL" ]; then
    echo -e "${RED}Nenhuma ferramenta xorriso/mkisofs/genisoimage encontrada.${NC}"
    exit 1
fi

echo -e "\n${YELLOW}A criar nova ISO com drivers VirtIO...${NC}"
echo -e "${YELLOW}Isto pode demorar vários minutos...${NC}"
if [ -n "$EFI_BOOT_REL" ]; then
    echo -e "${GREEN}A criar ISO com ${MKISO_TOOL_NAME} (BIOS + UEFI)...${NC}"
else
    echo -e "${GREEN}A criar ISO com ${MKISO_TOOL_NAME} (BIOS only)...${NC}"
fi

(
    cd "$ISO_EXTRACT" || exit 1

    MKISO_ARGS=(
        -iso-level 3
        -udf
        -D -N -relaxed-filenames -allow-limited-size
        -J -joliet-long
        -V "$ISO_LABEL"
        -b "$BOOT_IMG"
        -no-emul-boot
        -boot-load-size 8
        -boot-info-table
    )

    if [ -n "$EFI_BOOT_REL" ]; then
        MKISO_ARGS+=(
            -eltorito-alt-boot
            -eltorito-platform efi
            -eltorito-boot "$EFI_BOOT_REL"
            -no-emul-boot
        )
    fi

    MKISO_ARGS+=(
        -o "$NEW_ISO"
        .
    )

    # shellcheck disable=SC2086
    $MKISO_TOOL "${MKISO_ARGS[@]}"
)

# ---------- substituir ISO original ----------

if [ ! -f "$NEW_ISO" ]; then
    echo -e "${RED}Erro ao criar nova ISO${NC}"
    exit 1
fi

ISO_SIZE="$(du -h "$NEW_ISO" | cut -f1)"
echo -e "\n${GREEN}=== Sucesso! ===${NC}"
echo -e "${GREEN}Nova ISO criada com drivers VirtIO integrados${NC}"
echo -e "${GREEN}Tamanho: $ISO_SIZE${NC}"

ISO_DIR="$(dirname "$ISO_PATH")"
ISO_BASENAME="$(basename "$ISO_PATH" .iso)"
ORIGINAL_BACKUP="${ISO_DIR}/${ISO_BASENAME}-original.iso"

echo -e "\n${YELLOW}A fazer backup da ISO original...${NC}"
mv "$ISO_PATH" "$ORIGINAL_BACKUP"
echo -e "${GREEN}ISO original guardada em: $ORIGINAL_BACKUP${NC}"

echo -e "${YELLOW}A copiar nova ISO para o caminho original...${NC}"
cp "$NEW_ISO" "$ISO_PATH"

echo -e "\n${GREEN}=== Concluído! ===${NC}"
echo -e "${GREEN}ISO com drivers VirtIO: $ISO_PATH${NC}"
echo -e "${GREEN}ISO original (backup): $ORIGINAL_BACKUP${NC}"

echo -e "\n${GREEN}Instruções durante a instalação do Windows:${NC}"
echo -e "  1. Arranca a VM (SeaBIOS/UEFI) com esta ISO: $ISO_PATH"
echo -e "  2. Quando não aparecer nenhum disco, clica em \"Load driver\" / \"Carregar controlador\""
echo -e "  3. Vai à drive do DVD (normalmente D: ou E:)"
echo -e "  4. Para disco VirtIO normal (virtio-blk): D:\\drivers\\viostor\\w10\\amd64"
echo -e "  5. Para disco VirtIO-SCSI: D:\\drivers\\vioscsi\\w10\\amd64"
echo -e "  6. Marca \"Include subfolders\" / \"Incluir subpastas\" e avança."
echo -e "\n${GREEN}Feito. Podes agora instalar o Windows com discos VirtIO em modo BIOS ou UEFI.${NC}"
