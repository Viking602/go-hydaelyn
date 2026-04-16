# Plugin Development

## 插件模型

Hydaelyn 通过 `plugin.Registry` 统一管理插件，键格式为 `type/name`。

当前支持的插件类型：

- `provider`
- `tool`
- `planner`
- `verifier`
- `storage`
- `memory`
- `observer`
- `scheduler`
- `mcp_gateway`

## 注册方式

```go
err := runtime.RegisterPlugin(plugin.Spec{
	Type:      plugin.TypeProvider,
	Name:      "openai",
	Component: myProvider,
})
```

## 当前自动接线类型

- `provider`
- `tool`
- `storage`
- `observer`
- `scheduler`
- `mcp_gateway`

## 治理建议

插件优先接入：

- runtime middleware
- capability middleware

而不是在插件内部各写一套 timeout/retry/permission。
