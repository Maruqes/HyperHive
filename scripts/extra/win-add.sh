#!/usr/bin/env bash
# Cria um ISO de Windows 10/11 com drivers VirtIO incluídos numa pasta /virtio
# e, no fim, substitui o ISO original pelo novo, guardando o original como
# NOME-original.ext
#
# Uso:
#   chmod +x make_win_virtio.sh
#   ./make_win_virtio.sh
#
# Vai pedir:
#   - Caminho para o ISO original do Windows
#   - (Opcional) caminho para virtio-win.iso se não for encontrado automaticamente

set -euo pipefail

###############################################################################
# Helpers
###############################################################################

info()  { printf '[INFO] %s\n' "$*"; }
warn()  { printf '[WARN] %s\n' "$*" >&2; }
error() { printf '[ERROR] %s\n' "$*" >&2; exit 1; }

have_cmd() { command -v "$1" >/dev/null 2>&1; }

# sudo opcional
if [[ ${EUID:-$(id -u)} -eq 0 ]]; then
    SUDO=""
else
    SUDO="sudo"
fi

install_pkg() {
    local pkg="$1"

    if have_cmd dnf; then
        info "A instalar pacote '$pkg' com dnf..."
        $SUDO dnf install -y "$pkg"
    elif have_cmd apt-get; then
        info "A instalar pacote '$pkg' com apt-get..."
        $SUDO apt-get update -y
        $SUDO apt-get install -y "$pkg"
    else
        warn "Não sei instalar '$pkg' automaticamente nesta distro."
        warn "Instala manualmente e volta a correr o script."
    fi
}

VIRTIO_SEARCH_DIRS=(
    "/usr/share/virtio-win"
    "/usr/share/virtio"
    "/usr/lib/virtio-win"
    "/usr/lib64/virtio-win"
    "/usr/share/qemu"
)

search_virtio_iso() {
    local dir iso
    for dir in "${VIRTIO_SEARCH_DIRS[@]}"; do
        [[ -d "$dir" ]] || continue
        iso=$(find "$dir" -maxdepth 1 -type f -iname "virtio-win*.iso" -print -quit 2>/dev/null)
        if [[ -n "$iso" ]]; then
            printf '%s\n' "$iso"
            return 0
        fi
    done
    return 1
}

###############################################################################
# Ler caminho do ISO do Windows
###############################################################################

read -rp "Caminho para o ISO do Windows (ex: /path/Win10.iso): " WIN_ISO

[[ -f "$WIN_ISO" ]] || error "Ficheiro não encontrado: $WIN_ISO"

WIN_ISO=$(readlink -f "$WIN_ISO")
WIN_DIR=$(dirname "$WIN_ISO")
WIN_BASE=$(basename "$WIN_ISO")
WIN_EXT="${WIN_BASE##*.}"
WIN_BASENAME="${WIN_BASE%.*}"

OUT_ISO="$WIN_DIR/${WIN_BASENAME}-virtio.${WIN_EXT}"
BACKUP_ISO="$WIN_DIR/${WIN_BASENAME}-original.${WIN_EXT}"

info "ISO original: $WIN_ISO"
info "ISO de saída (temporário): $OUT_ISO"
info "Backup do original será:   $BACKUP_ISO"

###############################################################################
# Verificar / instalar dependências
###############################################################################

if ! have_cmd xorriso; then
    warn "xorriso não encontrado. Vou tentar instalar."
    install_pkg xorriso || true
fi

have_cmd xorriso || error "xorriso continua em falta. Instala-o manualmente e volta a correr o script."

# Tentar encontrar virtio-win.iso
VIRTIO_ISO=""

info "A procurar virtio-win.iso em locais conhecidos..."
if ! VIRTIO_ISO=$(search_virtio_iso); then
    warn "Não encontrei virtio-win.iso automaticamente."

    warn "Vou tentar instalar o pacote 'virtio-win' (se existir na tua distro)."
    install_pkg virtio-win || true

    if ! VIRTIO_ISO=$(search_virtio_iso); then
        warn "Ainda não encontrei virtio-win.iso automaticamente."
        read -rp "Caminho para virtio-win.iso (ou ENTER para abortar): " VIRTIO_MANUAL
        if [[ -z "$VIRTIO_MANUAL" ]]; then
            error "Sem virtio-win.iso não consigo continuar."
        fi
        [[ -f "$VIRTIO_MANUAL" ]] || error "Ficheiro não encontrado: $VIRTIO_MANUAL"
        VIRTIO_ISO=$(readlink -f "$VIRTIO_MANUAL")
    fi
fi

info "ISO de drivers VirtIO: $VIRTIO_ISO"

###############################################################################
# Preparar diretórios de trabalho
###############################################################################

WORKDIR="$(mktemp -d -t winvirtio.XXXXXXXX)"
MNT_WIN="$WORKDIR/mnt_win"
MNT_VIRTIO="$WORKDIR/mnt_virtio"
ISO_ROOT="$WORKDIR/iso_root"

mkdir -p "$MNT_WIN" "$MNT_VIRTIO" "$ISO_ROOT"

cleanup() {
    set +e
    info "A limpar diretórios temporários..."
    $SUDO umount "$MNT_WIN"    2>/dev/null || true
    $SUDO umount "$MNT_VIRTIO" 2>/dev/null || true
    rm -rf "$WORKDIR"
}
trap cleanup EXIT

###############################################################################
# Montar ISOs
###############################################################################

info "A montar ISO do Windows em $MNT_WIN"
$SUDO mount -o loop,ro "$WIN_ISO" "$MNT_WIN"

info "A montar ISO virtio-win em $MNT_VIRTIO"
$SUDO mount -o loop,ro "$VIRTIO_ISO" "$MNT_VIRTIO"

###############################################################################
# Copiar conteúdo do Windows + drivers VirtIO
###############################################################################

info "A copiar conteúdo do Windows para pasta de trabalho..."
cp -aT "$MNT_WIN" "$ISO_ROOT"

info "A copiar drivers VirtIO para pasta /virtio no ISO..."
mkdir -p "$ISO_ROOT/virtio"
cp -a "$MNT_VIRTIO"/. "$ISO_ROOT/virtio/"

###############################################################################
# Encontrar ficheiros de boot (BIOS + UEFI)
###############################################################################

cd "$ISO_ROOT"

BOOT_ETFS=$(find . -type f -iname "etfsboot.com" | head -n1 || true)
EFI_BOOT=$(find . -type f \( -iname "efisys.bin" -o -iname "efisys_noprompt.bin" \) | head -n1 || true)

[[ -n "$BOOT_ETFS" ]] || error "Não encontrei etfsboot.com no ISO do Windows."
[[ -n "$EFI_BOOT"  ]] || error "Não encontrei efisys.bin / efisys_noprompt.bin no ISO do Windows."

info "Ficheiro BIOS boot: $BOOT_ETFS"
info "Ficheiro UEFI boot: $EFI_BOOT"

###############################################################################
# Criar novo ISO
###############################################################################

VOLID="WIN_VIRTIO"

info "A criar ISO bootável com xorriso..."
xorriso -as mkisofs \
  -iso-level 3 \
  -full-iso9660-filenames \
  -volid "$VOLID" \
  -eltorito-boot "${BOOT_ETFS#./}" \
  -no-emul-boot -boot-load-size 8 -boot-info-table \
  -eltorito-alt-boot \
  -eltorito-platform efi \
  -eltorito-boot "${EFI_BOOT#./}" \
  -no-emul-boot \
  -output "$OUT_ISO" \
  .

info "Novo ISO criado: $OUT_ISO"

###############################################################################
# Substituir o ISO original e guardar backup
###############################################################################

# Se o backup já existir, acrescenta timestamp para não sobrescrever
if [[ -e "$BACKUP_ISO" ]]; then
    TS=$(date +%Y%m%d-%H%M%S)
    BACKUP_ISO="$WIN_DIR/${WIN_BASENAME}-original-${TS}.${WIN_EXT}"
    warn "Backup já existia, vou usar nome alternativo:"
    warn "  $BACKUP_ISO"
fi

info "A mover ISO original para backup: $BACKUP_ISO"
mv "$WIN_ISO" "$BACKUP_ISO"

info "A substituir ISO original pelo ISO com VirtIO..."
mv "$OUT_ISO" "$WIN_ISO"

info "Concluído!"
info "  Backup do original: $BACKUP_ISO"
info "  ISO atual (com pasta /virtio): $WIN_ISO"
info
info "Na VM:"
info "  - Disco em VirtIO (vda, bus=virtio)"
info "  - CD-ROM a apontar para: $WIN_ISO"
info "No setup do Windows:"
info "  - Clica em 'Load driver'"
info "  - Vai a D:\\virtio\\viostor\\w10\\amd64"
info "  - Instala o driver -> o disco VirtIO aparece e segues a instalação."
