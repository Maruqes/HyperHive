#!/usr/bin/env bash
# change-virsh-network.sh
# Muda o terceiro octeto da rede 'default' do libvirt (p.ex. 192.168.122.x -> 192.168.123.x)
# Usa: sudo ./change-virsh-network.sh
#    ou: sudo ./change-virsh-network.sh --yes   (não pede confirmação)

set -euo pipefail

AUTO_CONFIRM=0
if [ "${1:-}" = "--yes" ]; then
  AUTO_CONFIRM=1
fi

# check requisitos
if ! command -v virsh >/dev/null 2>&1; then
  echo "Erro: 'virsh' não encontrado. Instala libvirt/virsh antes."
  exit 1
fi

# precisa de permissões
if [ "$(id -u)" -ne 0 ]; then
  echo "Este script precisa de ser corrido com sudo/root."
  exit 1
fi

# confirma rede default existe
if ! virsh net-info default >/dev/null 2>&1; then
  echo "Rede 'default' não encontrada. Cria ou ativa-a antes."
  virsh net-list --all
  exit 1
fi

# dump do XML
TS=$(date +%Y%m%d-%H%M%S)
ORIG_XML="/tmp/libvirt-default-${TS}.xml"
MOD_XML="/tmp/libvirt-default-${TS}-mod.xml"
virsh net-dumpxml default > "${ORIG_XML}"
echo "Backup do XML original guardado em: ${ORIG_XML}"

# extrai o ip da rede (gateway)
CUR_IP=$(grep -oP "ip address='\K[0-9]+\.[0-9]+\.[0-9]+\.[0-9]+" "${ORIG_XML}" || true)
if [ -z "$CUR_IP" ]; then
  echo "Não consegui extrair o IP atual da rede do XML."
  exit 1
fi

IFS='.' read -r OCT1 OCT2 OCT3 OCT4 <<< "$CUR_IP"
NET_PREFIX="${OCT1}.${OCT2}"
echo "Rede actual detectada: ${CUR_IP}  (prefixo: ${NET_PREFIX}.x)"

# define limites aceitáveis para o terceiro octeto
MIN_OCTET=1
MAX_OCTET=254

echo ""
echo "Insere o terceiro octeto que queres usar (ex: 123 ou 200)."
echo "Aceitável: ${MIN_OCTET} a ${MAX_OCTET}  (0 e 255 são inválidos)."
while true; do
  read -rp "Insere um número entre ${MIN_OCTET} e ${MAX_OCTET}: " CHOSEN
  if ! [[ "$CHOSEN" =~ ^[0-9]+$ ]]; then
    echo "Valor inválido — não é número."
    continue
  fi
  if [ "$CHOSEN" -lt "$MIN_OCTET" ] || [ "$CHOSEN" -gt "$MAX_OCTET" ]; then
    echo "Valor fora do intervalo."
    continue
  fi
  break
done

if [ "$CHOSEN" -eq "$OCT3" ]; then
  echo "Aviso: escolheste o mesmo terceiro octeto já usado (${OCT3}). Nada a mudar."
  exit 0
fi

NEW_PREFIX="${NET_PREFIX}.${CHOSEN}"
NEW_GATEWAY="${NEW_PREFIX}.1"
NEW_RANGE_START="${NEW_PREFIX}.2"
NEW_RANGE_END="${NEW_PREFIX}.254"

echo ""
echo "Resumo da operação:"
echo " - Prefixo atual: ${NET_PREFIX}.${OCT3}.0"
echo " - Novo prefixo:  ${NEW_PREFIX}.0"
echo " - Gateway será: ${NEW_GATEWAY}"
echo " - DHCP range será: ${NEW_RANGE_START} -> ${NEW_RANGE_END}"
echo ""

if [ "$AUTO_CONFIRM" -ne 1 ]; then
  read -rp "Continuar e aplicar a alteração? (y/N): " CONF
  case "$CONF" in
    [yY][eE][sS]|[yY]) ;;
    *) echo "Abortado."; exit 0 ;;
  esac
fi

# prepara XML modificado: substitui occurrences do prefixo antigo (apenas a parte 3)
# substitui o ip address da tag <ip ...> e o range dentro do bloco dhcp
# fazemos substituição segura: trocamos todas as ocorrências do antigo prefixo por novo prefixo
OLD_PREFIX="${NET_PREFIX}.${OCT3}"
# cria o ficheiro modificado
cp "${ORIG_XML}" "${MOD_XML}"

# substituir linha do gateway (<ip address='x.x.x.1' netmask='...'>)
# e substituir quaisquer range start/end que usem o OLD_PREFIX
# Usamos sed com limites claros
sed -E -i \
  -e "s/${OLD_PREFIX}\.1/${NEW_GATEWAY}/g" \
  -e "s/${OLD_PREFIX}\.2/${NEW_RANGE_START}/g" \
  -e "s/${OLD_PREFIX}\.254/${NEW_RANGE_END}/g" \
  "${MOD_XML}"

# Caso o XML contenha range com end '... .254' mas com outro final, substituímos genericamente
# também substituímos todas as ocorrências do OLD_PREFIX. para NEW_PREFIX.
sed -E -i "s/${OLD_PREFIX}\./${NEW_PREFIX}\./g" "${MOD_XML}"

echo "XML modificado gerado em: ${MOD_XML}"
echo ""
echo "Mostrando diff (original -> modificado):"
if command -v diff >/dev/null 2>&1; then
  diff -u "${ORIG_XML}" "${MOD_XML}" || true
else
  echo "(diff não disponível)"
fi

# Aplica a nova rede: destroy, undefine, define, start, autostart
echo ""
echo "A aplicar a nova rede 'default' — isto vai derrubar a rede temporariamente."
virsh net-destroy default || echo "Aviso: não consegui destruir 'default' - pode já estar parada."
virsh net-undefine default || echo "Aviso: não consegui undefine 'default' (talvez já foi removed)."

virsh net-define "${MOD_XML}"
virsh net-start default
virsh net-autostart default

echo ""
echo "Feito. Nova rede 'default' iniciada com gateway ${NEW_GATEWAY} e DHCP ${NEW_RANGE_START}-${NEW_RANGE_END}."
echo "Verifica com:"
echo "  sudo virsh net-dumpxml default | sed -n '1,120p'"
echo "  ip addr show virbr0"
echo "As VMs ligadas a 'default' deverão receber novos IPs na sub-rede ${NEW_PREFIX}.x após renovar DHCP."
