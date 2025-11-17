#!/bin/bash

# Script para adicionar drivers VirtIO a uma ISO do Windows
# Autor: HyperHive
# Data: 2025

set -e

RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
NC='\033[0m' # No Color

echo -e "${GREEN}=== Script de Integração de Drivers VirtIO em ISO Windows ===${NC}\n"

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
    if ! command -v $dep &> /dev/null; then
        MISSING_DEPS+=($dep)
    fi
done

if [ ${#MISSING_DEPS[@]} -ne 0 ]; then
    echo -e "${RED}Dependências em falta: ${MISSING_DEPS[*]}${NC}"
    echo -e "${YELLOW}A instalar dependências...${NC}"
    
    # Instalar pacotes do Fedora
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
    PACKAGES_TO_INSTALL=($(echo "${PACKAGES_TO_INSTALL[@]}" | tr ' ' '\n' | sort -u | tr '\n' ' '))
    
    if [ ${#PACKAGES_TO_INSTALL[@]} -ne 0 ]; then
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

# Criar diretórios de trabalho temporários
WORK_DIR="/tmp/virtio_iso_work_$$"
ISO_MOUNT="$WORK_DIR/iso_mount"
ISO_EXTRACT="$WORK_DIR/iso_extract"
VIRTIO_DIR="$WORK_DIR/virtio"
OUTPUT_DIR="$WORK_DIR/output"

mkdir -p "$ISO_MOUNT" "$ISO_EXTRACT" "$VIRTIO_DIR" "$OUTPUT_DIR"

echo -e "\n${YELLOW}A criar diretórios de trabalho...${NC}"

# Montar a ISO original
echo -e "${YELLOW}A montar ISO original...${NC}"
mount -o loop "$ISO_PATH" "$ISO_MOUNT"

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
    cleanup
    exit 1
fi

# Montar ISO dos drivers VirtIO
echo -e "${YELLOW}A extrair drivers VirtIO...${NC}"
VIRTIO_MOUNT="$WORK_DIR/virtio_mount"
mkdir -p "$VIRTIO_MOUNT"
mount -o loop "$VIRTIO_ISO" "$VIRTIO_MOUNT"

# Copiar drivers para a ISO do Windows
echo -e "${YELLOW}A integrar drivers VirtIO na ISO...${NC}"

# Criar pasta para drivers
DRIVERS_DIR="$ISO_EXTRACT/drivers"
mkdir -p "$DRIVERS_DIR"

# Copiar drivers relevantes (suporta Windows 10, 11 e Server)
echo -e "${YELLOW}A copiar drivers...${NC}"
cp -r "$VIRTIO_MOUNT/NetKVM" "$DRIVERS_DIR/" 2>/dev/null || true
cp -r "$VIRTIO_MOUNT/viostor" "$DRIVERS_DIR/" 2>/dev/null || true
cp -r "$VIRTIO_MOUNT/vioscsi" "$DRIVERS_DIR/" 2>/dev/null || true
cp -r "$VIRTIO_MOUNT/Balloon" "$DRIVERS_DIR/" 2>/dev/null || true
cp -r "$VIRTIO_MOUNT/qemupciserial" "$DRIVERS_DIR/" 2>/dev/null || true
cp -r "$VIRTIO_MOUNT/qxldod" "$DRIVERS_DIR/" 2>/dev/null || true
cp -r "$VIRTIO_MOUNT/vioinput" "$DRIVERS_DIR/" 2>/dev/null || true
cp -r "$VIRTIO_MOUNT/viorng" "$DRIVERS_DIR/" 2>/dev/null || true
cp -r "$VIRTIO_MOUNT/viogpudo" "$DRIVERS_DIR/" 2>/dev/null || true

# Copiar guest tools se existirem
if [ -d "$VIRTIO_MOUNT/guest-agent" ]; then
    cp -r "$VIRTIO_MOUNT/guest-agent" "$DRIVERS_DIR/" 2>/dev/null || true
fi

umount "$VIRTIO_MOUNT"

# Criar ficheiro autounattend.xml para instalação automática dos drivers (opcional)
echo -e "${YELLOW}A criar ficheiro de configuração para drivers...${NC}"
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

# Obter informação da ISO original para criar a nova
ISO_NAME=$(basename "$ISO_PATH" .iso)
NEW_ISO="$OUTPUT_DIR/${ISO_NAME}_virtio.iso"

echo -e "\n${YELLOW}A criar nova ISO com drivers VirtIO...${NC}"
echo -e "${YELLOW}Isto pode demorar vários minutos...${NC}"

# Detectar tipo de boot e criar ISO
echo -e "${YELLOW}A detectar tipo de boot...${NC}"

# Procurar ficheiros de boot UEFI
EFISYS_BIN=""
if [ -f "$ISO_EXTRACT/efi/microsoft/boot/efisys.bin" ]; then
    EFISYS_BIN="efi/microsoft/boot/efisys.bin"
    echo -e "${GREEN}Detectado boot UEFI${NC}"
elif [ -f "$ISO_EXTRACT/EFI/microsoft/boot/efisys.bin" ]; then
    EFISYS_BIN="EFI/microsoft/boot/efisys.bin"
    echo -e "${GREEN}Detectado boot UEFI (maiúsculas)${NC}"
fi

# Procurar boot catalog
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

# Construir comando dinamicamente
MKISO_OPTS=(
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
    MKISO_OPTS+=(-b "$BOOT_CATALOG" -no-emul-boot -boot-load-size 8 -boot-info-table)
    if [ -n "$EFISYS_BIN" ]; then
        MKISO_OPTS+=(-eltorito-alt-boot -eltorito-platform efi -e "$EFISYS_BIN" -no-emul-boot)
    fi
elif [ -n "$EFISYS_BIN" ]; then
    MKISO_OPTS+=(-eltorito-boot "$EFISYS_BIN" -eltorito-platform efi -no-emul-boot)
fi

MKISO_OPTS+=(-o "$NEW_ISO" "$ISO_EXTRACT")

if ! "${MKISO_OPTS[@]}"; then
    echo -e "${RED}Falha ao gerar a ISO${NC}"
    exit 1
fi

# Verificar se a ISO foi criada
if [ -f "$NEW_ISO" ]; then
    ISO_SIZE=$(du -h "$NEW_ISO" | cut -f1)
    echo -e "\n${GREEN}=== Sucesso! ===${NC}"
    echo -e "${GREEN}Nova ISO criada com drivers VirtIO integrados${NC}"
    echo -e "${GREEN}Tamanho: $ISO_SIZE${NC}"
    
    # Fazer backup da ISO original
    ISO_DIR=$(dirname "$ISO_PATH")
    ISO_BASENAME=$(basename "$ISO_PATH" .iso)
    ORIGINAL_BACKUP="${ISO_DIR}/${ISO_BASENAME}-original.iso"
    
    echo -e "\n${YELLOW}A fazer backup da ISO original...${NC}"
    mv "$ISO_PATH" "$ORIGINAL_BACKUP"
    echo -e "${GREEN}ISO original guardada em: $ORIGINAL_BACKUP${NC}"
    
    # Copiar nova ISO para o caminho original
    echo -e "${YELLOW}A substituir ISO original pela versão com drivers...${NC}"
    cp "$NEW_ISO" "$ISO_PATH"
    
    echo -e "\n${GREEN}=== Concluído! ===${NC}"
    echo -e "${GREEN}ISO com drivers VirtIO: $ISO_PATH${NC}"
    echo -e "${GREEN}ISO original (backup): $ORIGINAL_BACKUP${NC}"
    echo -e "\n${GREEN}Instruções:${NC}"
    echo -e "1. Use a ISO em $ISO_PATH para instalar o Windows em VMs com VirtIO"
    echo -e "2. Durante a instalação, quando pedir drivers de disco, navegue para d:\\drivers\\viostor"
    echo -e "3. Os drivers de rede estarão em d:\\drivers\\NetKVM"
    echo -e "4. O ficheiro autounattend_drivers.xml pode ser usado para instalação automática"
    echo -e "5. A ISO original está guardada em: $ORIGINAL_BACKUP"
else
    echo -e "${RED}Erro ao criar nova ISO${NC}"
    exit 1
fi

# Limpeza
cleanup() {
    echo -e "\n${YELLOW}A limpar ficheiros temporários...${NC}"
    umount "$ISO_MOUNT" 2>/dev/null || true
    umount "$VIRTIO_MOUNT" 2>/dev/null || true
    rm -rf "$WORK_DIR"
    echo -e "${GREEN}Limpeza concluída${NC}"
}

cleanup

echo -e "\n${GREEN}=== Processo concluído com sucesso! ===${NC}"
