#!/usr/bin/env bash
# PostgreSQL 异地备份脚本（部署在生产服务器上运行，由 cron 每日 02:30 触发）
# 流程: pg_dump -Fc -> 本地 backups/ 留存 -> scp 传送到异地备份服务器 -> 双侧清理 7 天前旧备份
# 幂等: 可重复执行, 每次生成时间戳文件
#
# 2026-10-14 起支持双库: call-it-a-day 业务库并入本 PG 实例, 与 new-api 库同脚本备份。
#
# 环境变量(均可由 cron / make 注入, 以下为默认值):
#   APP_BASE      应用根目录          默认 /app/new-api
#   PG_DBS        数据库名列表(空格分隔) 默认继承 PG_DB, 再默认 new-api
#   PG_DB         (兼容旧 cron) 单库名  默认 new-api
#   PG_USER       数据库用户          默认 root
#   BACKUP_HOST   异地备份服务器      默认 139.224.65.205
#   BACKUP_PORT   异地 SSH 端口       默认 52222
#   BACKUP_USER   异地 SSH 用户       默认 root
#   REMOTE_DIR    异地留存目录        默认 /app/remote-backups/new-api
#   KEEP_DAYS     保留天数            默认 7
set -euo pipefail

APP_BASE=${APP_BASE:-/app/new-api}
PG_DBS=${PG_DBS:-${PG_DB:-new-api}}
PG_USER=${PG_USER:-root}
BACKUP_HOST=${BACKUP_HOST:-139.224.65.205}
BACKUP_PORT=${BACKUP_PORT:-52222}
BACKUP_USER=${BACKUP_USER:-root}
REMOTE_DIR=${REMOTE_DIR:-/app/remote-backups/new-api}
KEEP_DAYS=${KEEP_DAYS:-7}

LOCAL_DIR="$APP_BASE/backups"
TS=$(date +%Y%m%d_%H%M%S)

log() { printf '[%s] %s\n' "$(date '+%F %T')" "$*"; }

mkdir -p "$LOCAL_DIR"
cd "$APP_BASE"

ssh -p "$BACKUP_PORT" -o BatchMode=yes -o StrictHostKeyChecking=accept-new "$BACKUP_USER@$BACKUP_HOST" "mkdir -p '$REMOTE_DIR'"

for DB in $PG_DBS; do
    FILE="pg_${DB}_${TS}.dump"

    log ">>> 导出 PostgreSQL ($DB) -> $LOCAL_DIR/$FILE"
    # postgres 容器本地 socket 为 trust 认证, 无需密码
    docker compose exec -T postgres pg_dump -U "$PG_USER" -Fc "$DB" > "$LOCAL_DIR/$FILE"

    # 校验备份非空 (防止静默产出 0 字节坏备份)
    if [ ! -s "$LOCAL_DIR/$FILE" ] || [ "$(stat -c%s "$LOCAL_DIR/$FILE")" -lt 1024 ]; then
        log "!!! 备份文件异常(过小): $LOCAL_DIR/$FILE"
        rm -f "$LOCAL_DIR/$FILE"
        exit 1
    fi
    log ">>> 本地备份完成: $(du -h "$LOCAL_DIR/$FILE" | cut -f1)"

    log ">>> 传送到异地 $BACKUP_USER@$BACKUP_HOST:$REMOTE_DIR"
    scp -P "$BACKUP_PORT" -o BatchMode=yes "$LOCAL_DIR/$FILE" "$BACKUP_USER@$BACKUP_HOST:$REMOTE_DIR/"
done

log ">>> 清理 ${KEEP_DAYS} 天前旧备份 (本地 + 异地)"
find "$LOCAL_DIR" -name 'pg_*.dump' -mtime "+$KEEP_DAYS" -delete
ssh -p "$BACKUP_PORT" -o BatchMode=yes "$BACKUP_USER@$BACKUP_HOST" "find '$REMOTE_DIR' -name 'pg_*.dump' -mtime '+$KEEP_DAYS' -delete"

log ">>> 备份完成: $PG_DBS"
