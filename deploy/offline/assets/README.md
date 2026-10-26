# offline/assets

离线包需随包分发的依赖产物目录（骨架占位）：

- `k6`         压测引擎二进制（多平台）
- `chromium/`  headless Chromium（PDF 渲染）
- `postgres/`  PostgreSQL 离线安装包/二进制

构建期将上述产物放入本目录，build-offline.sh 或打包流程再拷贝到 `runtime/` 随包分发。
当前骨架阶段为空，具体二进制由实现阶段按目标平台补充。
