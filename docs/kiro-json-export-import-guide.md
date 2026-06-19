# Kiro JSON 导出与 Kiro-Go Plus 导入流程

本文档说明如何从本地 Kiro IDE 导出完整账号 JSON，并导入到 Kiro-Go Plus 验证使用。

## 适用范围

- 普通 Builder ID / IdC 账号。
- Google / GitHub social 账号。
- 企业组织账号，也就是 `external_idp` / Microsoft SSO / 企业 SSO 账号。

企业账号不能压缩成只含 `accessToken`、`refreshToken`、`clientId`、`clientSecret`、`region`、`provider`、`authMethod` 的七字段格式。该格式会丢失 `tokenEndpoint`、`issuerUrl`、`scopes`、`profileArn` 等字段，容易出现 400、`clientSecret` 缺失、`Invalid ARN` 或假导入。

## 一、先登录 Kiro IDE

1. 打开 Kiro IDE。
2. 使用目标账号完成登录。
3. 确认 Kiro IDE 里能正常显示账号和用量。
4. 如果是企业账号，旧 JSON 过期后必须重新登录再导出，不能反复导入旧 refreshToken。

## 二、导出完整 JSON

macOS / Linux:

```bash
python3 tools/export_kiro_account.py -o ~/Downloads/kiro-account-full.json
```

Windows:

```powershell
py tools\export_kiro_account.py -o "$env:USERPROFILE\Downloads\kiro-account-full.json"
```

如果脚本无法自动补 `profileArn`，但你已经知道正确 ARN，可以手动指定：

```bash
python3 tools/export_kiro_account.py \
  -o ~/Downloads/kiro-account-full.json \
  --profile-arn "arn:aws:codewhisperer:us-east-1:123456789012:profile/XXXXXXXXXXXX"
```

导出完成后，脚本只会在终端显示脱敏信息。不要把完整 JSON 发到公开群、GitHub、工单或截图里，因为里面有可复用凭据。

## 三、启动 Kiro-Go Plus

macOS:

```bash
cd macOS版
chmod +x ./kiro-go-plus
ADMIN_PASSWORD='改成你的强密码' ./kiro-go-plus
```

Windows PowerShell:

```powershell
cd Windows版
$env:ADMIN_PASSWORD="改成你的强密码"
.\kiro-go-plus.exe
```

注意：

- 终端窗口保持打开，服务才会继续运行。
- 浏览器打开 `http://127.0.0.1:8080/admin`。
- 不要打开 `http://0.0.0.0:8080/admin`，`0.0.0.0` 是监听地址，不是浏览器访问地址。
- 如果不设置 `ADMIN_PASSWORD`，默认密码不适合生产或客户环境。

## 四、导入到 Kiro-Go Plus

1. 打开 `http://127.0.0.1:8080/admin`。
2. 输入启动时设置的管理密码。
3. 进入账号管理。
4. 选择凭据 JSON 导入。
5. 选择第二步导出的完整 JSON 文件。

Kiro-Go Plus 支持以下结构：

- 单个 JSON 对象。
- 单账号数组：`[{...}]`。
- KAM 包装结构：`{"version":"merged","accounts":[{"credentials":{...}}]}`。

字段支持 camelCase 和 snake_case，例如 `refreshToken` / `refresh_token` 都可以识别。

## 五、导入后必须验证

导入成功只代表 JSON 被解析并保存，不代表账号已经可用。必须继续验证：

1. 刷新账号，确认能拿到用量。
2. 拉取模型列表，确认模型数量大于 0。
3. 点击测试或发起一次最小请求。

可用账号应满足：

- 用量接口返回 200。
- 模型接口返回 200。
- 测试请求返回 200。
- 账号区域、`authMethod`、`profileArn` 和企业账号元数据都被保留。

## 六、常见错误

### 1. `404 page not found`

通常是访问了 `0.0.0.0:8080/admin`。正确地址：

```text
http://127.0.0.1:8080/admin
```

### 2. `idc 模式需要同时提供 clientId 和 clientSecret`

通常是企业账号被错误识别成 `idc`，或者导入 JSON 丢了 `authMethod` / `tokenEndpoint`。重新使用完整 JSON 导入，不要手动编造 `clientSecret`。

### 3. `credentials.refreshToken missing`

这是另一个工具需要的 KAM 结构，不是 Kiro-Go Plus 的错误。请确认导入入口和 JSON 结构匹配。

### 4. `Invalid ARN` / `Invalid profileArn`

常见原因：

- `profileArn` 缺失。
- JSON 的 `region` 和 ARN 区域不一致。
- 企业账号请求缺少 `TokenType: EXTERNAL_IDP`。
- 拿了另一个账号的 `profileArn`。

使用新版 Kiro-Go Plus，并用重新登录后导出的完整 JSON 导入。

### 5. `Invalid token provided` / OIDC 400

通常是 refreshToken 已轮换或失效。解决方式是重新登录 Kiro IDE，再重新导出完整 JSON。

## 七、不要做的事

- 不要把企业账号转成七字段简化格式。
- 不要把账号 JSON、`data/config.json`、日志、token 文件上传到 GitHub。
- 不要把完整 token 截图发给客户或公开渠道。
- 不要把 `0.0.0.0` 当浏览器地址。

## 八、推荐交付给客户的内容

- 对应平台的 `kiro-go-plus` 二进制文件。
- `tools/export_kiro_account.py`。
- 本文档。
- `LICENSE` 和 `UPSTREAM.md`。

不要交付本机 `data/config.json`、账号 JSON 样本、Kiro IDE 缓存或任何真实凭据。
