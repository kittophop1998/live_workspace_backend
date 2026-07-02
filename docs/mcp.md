# คู่มือใช้งาน MCP

Backend สามารถเปิด Model Context Protocol (MCP) ผ่าน HTTP ควบคู่กับ REST API
ได้ โดยค่าเริ่มต้น MCP จะปิดอยู่และเครื่องมือทั้งหมดเป็นแบบอ่านข้อมูลอย่างเดียว

## 1. เปิดใช้งาน

กำหนดค่าใน `.env`:

```env
MCP_ENABLED=true
MCP_PATH=/mcp
```

จากนั้นเริ่ม backend ตามปกติ:

```bash
set -a
source .env
set +a
go run ./cmd/api
```

เมื่อใช้ค่าตั้งต้น MCP endpoint จะอยู่ที่:

```text
http://localhost:8080/mcp
```

`MCP_PATH` ต้องขึ้นต้นด้วย `/` และห้ามเป็น `/` หาก `MCP_ENABLED=false`
ระบบจะไม่ register route นี้และการเรียก endpoint จะได้ `404`

## 2. ขอ access token

MCP ใช้ JWT ตัวเดียวกับ REST API ผู้ใช้ต้องเป็น collaborator ของ workspace
ที่อยู่ใน token ด้วย

สร้างห้องใหม่:

```bash
curl -X POST http://localhost:8080/api/v1/rooms \
  -H 'Content-Type: application/json' \
  -d '{"name":"Alice"}'
```

หรือเข้าห้องเดิม:

```bash
curl -X POST http://localhost:8080/api/v1/rooms/join \
  -H 'Content-Type: application/json' \
  -d '{"room_code":"123456","name":"Bob"}'
```

นำค่า `access_token` จาก response ไปใช้เป็น Bearer token ส่วนค่า workspace
ที่ต้องส่งเป็น `project_id` คือ `room_code` ที่ได้รับจาก response เดียวกัน

เพื่อให้ตัวอย่างถัดไปเรียกใช้ง่าย สามารถกำหนดตัวแปรไว้ก่อน:

```bash
export MCP_URL=http://localhost:8080/mcp
export ACCESS_TOKEN='<access_token>'
export PROJECT_ID='<room_code>'
```

## 3. ตรวจสอบการเชื่อมต่อ

เรียก `initialize`:

```bash
curl -X POST "$MCP_URL" \
  -H "Authorization: Bearer $ACCESS_TOKEN" \
  -H 'Content-Type: application/json' \
  -d '{
    "jsonrpc": "2.0",
    "id": 1,
    "method": "initialize",
    "params": {
      "protocolVersion": "2025-03-26",
      "capabilities": {},
      "clientInfo": {
        "name": "curl",
        "version": "1.0.0"
      }
    }
  }'
```

ดูรายการ tools:

```bash
curl -X POST "$MCP_URL" \
  -H "Authorization: Bearer $ACCESS_TOKEN" \
  -H 'Content-Type: application/json' \
  -d '{"jsonrpc":"2.0","id":2,"method":"tools/list"}'
```

เรียก tool ตัวอย่าง:

```bash
curl -X POST "$MCP_URL" \
  -H "Authorization: Bearer $ACCESS_TOKEN" \
  -H 'Content-Type: application/json' \
  -d "{
    \"jsonrpc\": \"2.0\",
    \"id\": 3,
    \"method\": \"tools/call\",
    \"params\": {
      \"name\": \"listEndpoints\",
      \"arguments\": {
        \"project_id\": \"$PROJECT_ID\"
      }
    }
  }"
```

Server รองรับ JSON-RPC methods ต่อไปนี้:

- `initialize`
- `notifications/initialized`
- `ping`
- `tools/list`
- `tools/call`

รองรับเฉพาะ HTTP `POST` ตัว request มีขนาดได้ไม่เกิน 1 MiB และ server
ทำงานแบบ stateless จึงไม่ต้องเก็บ session ID

## 4. ตั้งค่า MCP client

สำหรับ client ที่รองรับ remote HTTP MCP และ custom headers ให้ตั้งค่าในรูปแบบนี้:

```json
{
  "mcpServers": {
    "fark-noi": {
      "url": "http://localhost:8080/mcp",
      "headers": {
        "Authorization": "Bearer <access_token>"
      }
    }
  }
}
```

ตำแหน่งไฟล์ config และชื่อ property อาจต่างกันในแต่ละ client แต่ค่าที่จำเป็นมี
เพียง URL และ `Authorization` header หาก client บังคับใช้ Streamable HTTP
แบบเต็มรูปแบบ เช่น ต้องเรียก `GET` หรือใช้ session transport จะยังเชื่อมต่อกับ
endpoint นี้ไม่ได้ ให้ทดสอบด้วยคำสั่ง `curl` ด้านบนก่อน

## 5. Tools ที่มี

| Tool | Arguments | ใช้สำหรับ |
| --- | --- | --- |
| `listProjects` | ไม่มี | ดู project ที่ token นี้เข้าถึงได้ |
| `getProject` | `project_id` | ดู snapshot ของ workspace |
| `listEndpoints` | `project_id` | ดู endpoint resources ทั้งหมด |
| `getEndpoint` | `project_id`, `endpoint_id` | ดู endpoint รายการเดียว |
| `getOpenAPISpec` | `project_id` | สร้าง OpenAPI 3.1 จาก endpoint resources |
| `getJSONSchema` | `project_id`, `resource_id` | สร้าง JSON Schema ของ resource |
| `getWorkflow` | `project_id`, `workflow_id` | ดู workflow ที่บันทึกไว้ |
| `listComments` | `project_id`, `endpoint_id`, `field_id` (ไม่บังคับ) | ดู comments ของ resource หรือ field |

ข้อควรจำ:

- ระบบปัจจุบันใช้ Live Workspace เป็นขอบเขตของ project ดังนั้น `project_id`
  ต้องเท่ากับ workspace ID หรือ `room_code` ใน JWT
- ไม่สามารถใช้ token อ่าน project ของ workspace อื่นได้
- Arguments ที่ไม่อยู่ใน schema จะถูกปฏิเสธ
- ผลลัพธ์ของ tool อยู่ใน `result.content[0].text` และเป็น JSON ที่ encode
  เป็น string อีกชั้นหนึ่ง
- Tool error จะตอบเป็น JSON-RPC response ปกติ โดยมี `result.isError=true`

## 6. ข้อจำกัดและความปลอดภัย

- Tools ทั้งหมดเป็น read-only ไม่มี create, update, delete, execute workflow
  หรือการเขียนฐานข้อมูล
- ทุก request ตรวจ JWT และ collaborator membership ใหม่เสมอ
- Server ไม่บันทึก token, secret หรือ tool arguments ลง MCP request log
- Server บันทึกเฉพาะชื่อ tool, user ID, workspace ID, project ID, ระยะเวลา
  และสถานะ error
- OpenAPI และ JSON Schema ถูกสร้างจากข้อมูล resource ปัจจุบัน ไม่ได้เป็น spec
  ที่บันทึกแยกต่างหาก
- OpenAPI ที่สร้างขึ้นยังไม่อนุมาน parameters, security schemes, servers
  และ error responses ที่ไม่มีอยู่ใน workspace model

## 7. แก้ปัญหาเบื้องต้น

| อาการ | สาเหตุที่ควรตรวจ |
| --- | --- |
| `404` | ยังไม่ได้ตั้ง `MCP_ENABLED=true`, ยังไม่ได้ restart หรือ `MCP_PATH` ไม่ตรง |
| `401` | ไม่มี Bearer token, token ผิด, หมดอายุ หรือ `JWT_SECRET` ไม่ตรงกับตอนออก token |
| `unauthorized: user is not a member...` | collaborator ใน token ไม่อยู่ใน workspace แล้ว |
| `unauthorized: project is outside...` | `project_id` ไม่ตรงกับ workspace/room ใน token |
| `validation failed: invalid arguments` | ชื่อ field ผิด, ขาด argument หรือส่ง argument เกิน schema |
| client ต่อไม่ได้แต่ `curl` ใช้ได้ | client อาจต้องการ MCP transport feature ที่ endpoint นี้ยังไม่รองรับ |

หลังแก้โค้ด MCP ให้ตรวจสอบด้วย:

```bash
go test ./internal/adapter/mcp ./internal/config
go test ./...
```
