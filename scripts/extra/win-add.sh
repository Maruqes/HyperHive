#!/usr/bin/env bash

# Script para adicionar drivers VirtIO a uma ISO do Windows
# Autor: HyperHive
# Data: 2025

set -euo pipefail

RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
NC='\033[0m' # No Color

echo -e "${GREEN}=== Script de Integração de Drivers VirtIO em ISO Windows ===${NC}\n"

# Variáveis globais para cleanup
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

# Verificar se está a correr como root
if [ "$EUID" -ne 0 ]; then 
    echo -e "${RED}Por favor, execute como root (sudo)${NC}"
    exit 1
fi

# Verificar dependências necessárias
echo -e "${YELLOW}A verificar dependências...${NC}"
DEPENDENCIES=("genisoimage" "mkisofs" "wget" "7z" "rsync")
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
            "wget")
                PACKAGES_TO_INSTALL+=("wget")
                ;;
            "7z")
                PACKAGES_TO_INSTALL+=("p7zip" "p7zip-plugins")
                ;;
            "rsync")
                PACKAGES_TO_INSTALL+=("rsync")
                ;;
        esac
    done

    # Remover duplicados
    if [ ${#PACKAGES_TO_INSTALL[@]} -ne 0 ]; then
        read -r -a PACKAGES_TO_INSTALL <<< "$(printf '%s\n' "${PACKAGES_TO_INSTALL[@]}" | sort -u)"
        dnf install -y --skip-unavailable "${PACKAGES_TO_INSTALL[@]}"
    fi
fi

# Pedir caminho da ISO ao utilizador
echo -e "\n${GREEN}Por favor, indique o caminho completo da ISO do Windows:${NC}"
read -e -p "Caminho da ISO: " ISO_PATH

# Validar se o ficheiro existe
if [ ! -f "$ISO_PATH" ]; then
    echo -e "${RED}Erro: Ficheiro não encontrado: $ISO_PATH${NC}"
    exit 1
fi

# Validar se é um ficheiro ISO
if [[ ! "$ISO_PATH" =~ \.iso$ ]]; then
    echo -e "${RED}Erro: O ficheiro não é uma ISO${NC}"
    exit 1
fi

echo -e "${GREEN}ISO encontrada: $ISO_PATH${NC}"

# Diretório base para trabalho (muda aqui se quiseres outro disco)
WORK_BASE="${WORK_BASE:-/mnt/512SvMan/shared/virtio_iso_work}"
mkdir -p "$WORK_BASE"

# Criar diretório de trabalho único
WORK_DIR="$(mktemp -d "$WORK_BASE/virtio_iso_work_XXXXXX")"
ISO_MOUNT="$WORK_DIR/iso_mount"
ISO_EXTRACT="$WORK_DIR/iso_extract"
VIRTIO_DIR="$WORK_DIR/virtio"
OUTPUT_DIR="$WORK_DIR/output"
VIRTIO_MOUNT="$WORK_DIR/virtio_mount"

mkdir -p "$ISO_MOUNT" "$ISO_EXTRACT" "$VIRTIO_DIR" "$OUTPUT_DIR" "$VIRTIO_MOUNT"

echo -e "\n${YELLOW}Diretório de trabalho: $WORK_DIR${NC}"

# Montar a ISO original (read-only)
echo -e "${YELLOW}A montar ISO original...${NC}"
mount -o loop,ro "$ISO_PATH" "$ISO_MOUNT"

# Copiar conteúdo da ISO
echo -e "${YELLOW}A extrair conteúdo da ISO... (isto pode demorar)${NC}"
rsync -av --progress "$ISO_MOUNT/" "$ISO_EXTRACT/"

umount "$ISO_MOUNT"

# Tornar os ficheiros editáveis
chmod -R u+w "$ISO_EXTRACT"

# Descarregar drivers VirtIO mais recentes
echo -e "\n${YELLOW}A descarregar drivers VirtIO...${NC}"
VIRTIO_ISO_URL="https://fedorapeople.org/groups/virt/virtio-win/direct-downloads/stable-virtio/virtio-win.iso"
VIRTIO_ISO="$WORK_DIR/virtio-win.iso"

wget -c "$VIRTIO_ISO_URL" -O "$VIRTIO_ISO"

if [ ! -f "$VIRTIO_ISO" ]; then
    echo -e "${RED}Erro ao descarregar drivers VirtIO${NC}"
    exit 1
fi

# Montar ISO dos drivers VirtIO
echo -e "${YELLOW}A montar ISO dos drivers VirtIO...${NC}"
mount -o loop,ro "$VIRTIO_ISO" "$VIRTIO_MOUNT"

# Criar pasta para drivers na ISO do Windows
DRIVERS_DIR="$ISO_EXTRACT/drivers"
mkdir -p "$DRIVERS_DIR"

echo -e "${YELLOW}A copiar drivers VirtIO...${NC}"
for dir in NetKVM viostor vioscsi Balloon qemupciserial qxldod vioinput viorng viogpudo guest-agent; do
    if [ -d "$VIRTIO_MOUNT/$dir" ]; then
        cp -a "$VIRTIO_MOUNT/$dir" "$DRIVERS_DIR/"
    fi
done

umount "$VIRTIO_MOUNT"

# Criar ficheiro autounattend_drivers.xml (opcional)
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

# Obter informação da ISO original
ISO_NAME="$(basename "$ISO_PATH" .iso)"
NEW_ISO="$OUTPUT_DIR/${ISO_NAME}_virtio.iso"

echo -e "\n${YELLOW}A criar nova ISO com drivers VirtIO...${NC}"
echo -e "${YELLOW}Isto pode demorar vários minutos...${NC}"

# Detectar ficheiros de boot
EFISYS_BIN=""
if [ -f "$ISO_EXTRACT/efi/microsoft/boot/efisys.bin" ]; then
    EFISYS_BIN="efi/microsoft/boot/efisys.bin"
    echo -e "${GREEN}Detectado boot UEFI${NC}"
elif [ -f "$ISO_EXTRACT/EFI/microsoft/boot/efisys.bin" ]; then
    EFISYS_BIN="EFI/microsoft/boot/efisys.bin"
    echo -e "${GREEN}Detectado boot UEFI (maiúsculas)${NC}"
fi

BOOT_CATALOG=""
if [ -f "$ISO_EXTRACT/boot/etfsboot.com" ]; then
    BOOT_CATALOG="boot/etfsboot.com"
    echo -e "${GREEN}Detectado boot BIOS${NC}"
fi

# Escolher ferramenta para gerar ISO
MKISO_TOOL=""
if command -v genisoimage &> /dev/null; then
    MKISO_TOOL="$(command -v genisoimage)"
elif command -v mkisofs &> /dev/null; then
    MKISO_TOOL="$(command -v mkisofs)"
fi

if [ -z "$MKISO_TOOL" ]; then
    echo -e "${RED}Nenhuma ferramenta mkisofs/genisoimage encontrada.${NC}"
    exit 1
fi

echo -e "${GREEN}A criar ISO com ${MKISO_TOOL##*/}...${NC}"

# Criar ISO a partir de dentro da pasta extraída
(
    cd "$ISO_EXTRACT" || exit 1

    CMD=(
        "$MKISO_TOOL"
        -iso-level 3
        -udf
        -D
        -N
        -relaxed-filenames
        -allow-limited-size
        -J
        -joliet-long
        -V "Windows_VirtIO"
    )

    if [ -n "$BOOT_CATALOG" ]; then
        CMD+=(
            -b "$BOOT_CATALOG"
            -no-emul-boot
            -boot-load-size 8
            -boot-info-table
        )
    fi

    if [ -n "$EFISYS_BIN" ]; then
        CMD+=(
            -eltorito-alt-boot
            -e "$EFISYS_BIN"
            -no-emul-boot
        )
    fi


    CMD+=(
        -o "$NEW_ISO"
        .
    )

    echo "Comando final:"
    printf ' %q' "${CMD[@]}"
    echo

    "${CMD[@]}"
)

# Verificar se a ISO foi criada
if [ ! -f "$NEW_ISO" ]; then
    echo -e "${RED}Erro ao criar nova ISO${NC}"
    exit 1
fi

ISO_SIZE="$(du -h "$NEW_ISO" | cut -f1)"
echo -e "\n${GREEN}=== Sucesso! ===${NC}"
echo -e "${GREEN}Nova ISO criada com drivers VirtIO integrados${NC}"
echo -e "${GREEN}Tamanho: $ISO_SIZE${NC}"

# Fazer backup da ISO original e substituir
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
echo -e "  1. Arranca a VM com esta ISO: $ISO_PATH"
echo -e "  2. Quando não aparecer nenhum disco, clica em \"Load driver\" / \"Carregar controlador\""
echo -e "  3. Vai à drive do DVD (normalmente D: ou E:)"
echo -e "  4. Para disco VirtIO normal (virtio-blk): navega até D:\\drivers\\viostor\\w10\\amd64"
echo -e "  5. Para disco VirtIO-SCSI: navega até D:\\drivers\\vioscsi\\w10\\amd64"
echo -e "  6. Marca a opção \"Include subfolders\" / \"Incluir subpastas\" e avança."
echo -e "\n${GREEN}Feito. Podes agora instalar o Windows com discos VirtIO.${NC}"
