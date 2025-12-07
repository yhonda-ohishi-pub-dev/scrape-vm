# Cloud Run Deploy

postgres-prod を Cloud Run にデプロイします。

## 手順

1. Cloud Build でイメージをビルド・プッシュ:
```bash
gcloud builds submit --config=cloudbuild.yaml --project=cloudsql-sv
```

2. Cloud Run にデプロイ:
```bash
gcloud run deploy postgres-prod \
  --image=asia-northeast1-docker.pkg.dev/cloudsql-sv/postgres-prod/postgres-prod:latest \
  --region=asia-northeast1 \
  --platform=managed \
  --allow-unauthenticated \
  --add-cloudsql-instances=cloudsql-sv:asia-northeast1:postgres-prod \
  --set-env-vars=GCP_PROJECT_ID=cloudsql-sv,GCP_REGION=asia-northeast1,CLOUDSQL_INSTANCE_NAME=postgres-prod,DB_NAME=myapp,DB_USER=747065218280-compute@developer \
  --use-http2 \
  --project=cloudsql-sv
```

3. デプロイ完了後、Service URL を表示

## 注意事項
- cloudbuild.yaml の service-resolved.yaml デプロイステップはYAMLパースエラーがあるため、直接 gcloud run deploy を使用
- イメージビルドは cloudbuild.yaml で行い、デプロイは直接コマンドで実行
