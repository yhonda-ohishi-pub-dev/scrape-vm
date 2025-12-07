## 実行手順

1. **plan.md確認**: 「次のステップ（予定）」のタスクを取得 必要なデータを取得する際は、コンテクストむだに消費しないためにすべて読みにいかず、一部だけ読み込む
2. **タスク実行**: 各タスクをsub agentで並列実行　sub agentで実行する前にmainで一度実行しないとsub agentに権限が渡せない
3. **plan.md更新**:
   - 完了タスクを「完了: [タスク名] (日付)」セクションに移動
   - 以下を必ず記載：
     - コミットハッシュ（あれば）
     - 作成ファイル一覧
     - 更新ファイル一覧
     - テスト結果（あれば）
4. **PLAN-EXECUTED.md移行**: plan.mdの完了セクションを`docs/PLAN-EXECUTED.md`に移動
5. **plan.md整理**: 「次のステップ（予定）」のみ残す
6. **gitコミット**: 日本語でコミットメッセージを作成

## コミットメッセージ形式

```
[タスク名]を追加/実装/修正

- 変更点1
- 変更点2

🤖 Generated with [Claude Code](https://claude.com/claude-code)

Co-Authored-By: Claude Opus 4.5 <noreply@anthropic.com>
```
