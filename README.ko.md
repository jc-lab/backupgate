# BackupGate

BackupGate는 대용량 백업 파일을 HTTP 또는 S3 호환 API로 받아 기반 원격 스토리지(NAS 등)에 저장하는 백업 게이트웨이입니다.
key(path) 단위로 인증, 버퍼링, 무결성 검증, rotation 정책을 분리해 운영할 수 있습니다.

## 주요 기능

- HTTP `POST /{key}` 업로드
- S3 호환 단일 객체 `PUT /{key}` 업로드
- S3 multipart upload (`FULLY_COMPLETE` 모드)
- Basic Auth 및 AWS Signature V4 인증
- inline/file/htpasswd/external 인증 provider
- SHA256 header 검증, MD5 ETag 응답, 원격 다운로드 기반 재검증
- 실패 시 원격 크기 기준 resume upload 재시도
- key별 count/age rotation

## Storage Backend

- sftp

## 빌드

```bash
go build -o backupgate ./cmd/backupgate
```

## 실행

```bash
./backupgate -config backupgate.yaml
```

버전만 확인하려면:

```bash
./backupgate --version
```

환경변수로도 기본값을 줄 수 있습니다.

- `BACKUPGATE_CONFIG`: 설정 파일 경로를 지정합니다. `-config` 플래그보다 우선하지는 않지만, 플래그를 생략했을 때 기본 경로로 사용됩니다.
- `BACKUPGATE_LOG_LEVEL`: 로그 레벨을 지정합니다. 예: `debug`, `info`, `warn`, `error`

`server.debug.listen`을 설정하면 별도 디버그 서버가 뜨고, `GET /debug/health`는 `{"status":"UP"}` JSON을 반환합니다.
`GET /debug/version`은 빌드된 버전, commit, build date를 JSON으로 반환합니다.

## 설정 예시

```yaml
server:
  api:
    mode: header
    listen: ":8080"
  debug:
    listen: ":5000"

buffer:
  memory: 67108864
  max_memory: 1073741824
  sliding_window_size: 1048576
  tmp_dir: /tmp

storage:
  type: sftp
  sftp:
    host: nas.example.com:22
    username: backup
    key_file: /etc/backupgate/id_ed25519
    base_path: /volume1/backups

keys:
  default:
    buffer_mode: FULLY_COMPLETE
    verification: true
    rotation:
      max_count: 7
      max_age: 168h
    auth:
      - type: inline
        credentials:
          - username: backup-agent
            password: "${env.BACKUP_AGENT_PASSWORD}"
  database:
    buffer_mode: FULLY_COMPLETE
    verification: true
    rotation:
      max_count: 7
      max_age: 168h
    auth:
      - type: inline
        credentials:
          - username: backup-agent
            password: "${env.BACKUP_AGENT_PASSWORD}"
```

Port 분리 모드는 다음처럼 설정합니다.

```yaml
server:
  api:
    mode: port
    s3:
      listen: ":9000"
    http:
      listen: ":8080"
```

~~SFTP storage와 rsync 업로드 가속 예시:~~

- 아직 지원하지 않습니다.

```yaml
storage:
  type: sftp
  sftp:
    host: nas.example.com:22
    username: backup
    key_file: /etc/backupgate/id_ed25519
    base_path: /backups
  rsync:
    enabled: true
    base_path: /volume1/backups
```

storage backend는 SFTP만 지원합니다. `storage.rsync.enabled`가 `true`이면 업로드에 한해서만 rsync protocol을 사용하고, 다운로드/삭제/리스팅/rotation은 SFTP로 수행합니다. SFTP와 rsync의 `base_path`는 서로 다른 경로 체계를 같은 저장 위치에 매핑할 수 있도록 각각 설정합니다.

rsync 업로드는 로컬 PC의 `ssh(1)` 또는 `rsync(1)` 바이너리를 실행하지 않습니다. BackupGate 프로세스가 SFTP용 Go SSH client를 공유하고, SSH session의 stdin/stdout 위에서 `github.com/gokrazy/rsync`의 rsync protocol client를 실행합니다. 원격 서버에는 rsync server 역할로 실행될 `rsync` 명령이 PATH에 있어야 합니다.

rsync 업로드를 활성화하면 모든 key는 `FULLY_COMPLETE` buffer mode만 사용할 수 있습니다. 이 모드에서는 업로드 payload가 file-backed buffer에 저장되며, rsync 전송 시 별도 임시 복사본을 만들지 않고 해당 buffer 파일을 직접 사용합니다.

## HTTP 업로드

```bash
SHA256=$(sha256sum backup.tar.zst | awk '{print $1}')
curl -u "backup-agent:${BACKUP_PASSWORD}" \
  -H "X-Backup-SHA256: ${SHA256}" \
  --data-binary @backup.tar.zst \
  http://localhost:8080/database/production-daily
```

성공 시 `201 Created`와 함께 저장 경로, 크기, SHA256, MD5, 검증 여부가 JSON으로 반환되며 `ETag: "{md5 digest hex}"` header가 포함됩니다.

## S3 호환 업로드

S3 API는 AWS Signature V4 `Authorization` header를 검증합니다. 설정의 인증 username은 access key, password는 secret key로 사용됩니다.

```bash
aws --endpoint-url http://localhost:9000 s3 cp backup.tar.zst s3://backupgate/db/production-daily
```

단일 포트 header mode에서는 `Authorization: AWS4-HMAC-SHA256 ...` 요청은 S3로, 그 외 요청은 HTTP API로 라우팅됩니다.

bucket location 요청은 호환성을 위해 dummy location을 반환합니다.

```bash
curl "http://localhost:9000/backupgate/?location"
```

## 인증 Provider

inline:

```yaml
auth:
  - type: inline
    credentials:
      - username: admin
        password: "${env.ADMIN_PASSWORD}"
```

file:

```yaml
auth:
  - type: file
    path: /etc/backupgate/credentials.yaml
```

htpasswd:

```yaml
auth:
  - type: htpasswd
    path: /etc/backupgate/htpasswd
```

external:

```yaml
auth:
  - type: external
    command: /usr/local/bin/backupgate-auth
    args: ["--username", "%u", "--password", "%p", "--key", "%k"]
    timeout: 5s
```

## Buffer Mode

- `FULLY_IMMEDIATELY`: 전체 payload를 upload reader에 보관한 뒤 업로드하며, 실패 시 resume/retry와 검증 실패 재전송을 수행합니다.
- `FULLY_COMPLETE`: 전체 payload SHA256을 먼저 확인한 뒤 업로드합니다. S3 multipart는 이 모드에서만 허용됩니다.
- `OFF`: 요청 body를 storage backend로 직접 스트리밍합니다. SHA256은 전송 중 계산하며, 검증 실패 시 재전송하지 않고 오류로 처리합니다.

## Rotation

업로드 파일은 다음 형식으로 저장됩니다.

```text
{dir of key}/20060102T150405Z_{filename}
```

예:

```text
db/20260429T121530Z_production-daily.sql.zst
```

rotation은 파일명 앞의 timestamp(`20060102T150405Z`)가 있는 파일만 대상으로 하며, 그 timestamp를 기준으로 정렬합니다. timestamp가 없는 파일은 rotation 대상에서 제외됩니다.

# License

AGPL-3.0-only
