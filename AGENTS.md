# quicsmb 规则（对齐 mytun）

- 先让它跑起来：最小改动解决问题
- 每次修复/新功能都走 TDD 红绿灯流程
- 无状态优先：纯函数做判定，状态压成薄胶水
- 做减法：加东西前先删复杂度；一次只做一个可验证的小单元
- 修根因：掩盖崩溃的 guard 不是修；测试行为不测实现
- 单测不要依赖网络/外部，有需要 mock 掉。提交前 `make test-all` 全绿。
- 前端：WebComponent 一组件一文件，vitest 先 RED 后 GREEN；用全局工具链，不写 web/package.json、不提交 node_modules。
