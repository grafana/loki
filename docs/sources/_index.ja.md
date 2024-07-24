---
title: Grafana Loki
description: Grafana Lokiは、フル機能のログスタックを構成することができるオープンソースのコンポーネント群です。
outputs:
  - HTML
  - menu
hero:
  description: Grafana Lokiは、フル機能のログスタックを構成することができるオープンソースのコンポーネント群です。小さなインデックスと高度に圧縮されたチャンクにより、運用が簡素化され、Lokiのコストが大幅に削減されます。
cards:
  items:
    - title: Lokiについて学ぶ
      href: /docs/loki/latest/get-started/
      description: Lokiのアーキテクチャとコンポーネント、さまざまなデプロイメントモード、およびラベルのベストプラクティスについて学ぶ。
    - title: Lokiの設定
      href: /docs/loki/latest/setup/
      description: Lokiの構成およびインストール方法、以前のデプロイメントからの移行方法、およびLoki環境のアップグレード方法に関する指示を確認してください。
    - title: Lokiの構成
      href: /docs/loki/latest/configure/
      description: Lokiの構成リファレンスと構成例を参照してください。
    - title: ログをLokiに送信する
      href: /docs/loki/latest/send-data/
      description: Lokiにログを送信するために使用するクライアントを1つ以上選択してください。
    - title: Lokiの管理
      href: /docs/loki/latest/operations/
      description: テナントの管理、ログの取り込み、ストレージ、クエリなどの管理方法を学びましょう。
    - title: LogQLを使ってクエリを実行する
      href: /docs/loki/latest/query/
      description: PromQLにインスパイアされたLogQLは、Grafana Lokiのクエリ言語です。LogQLはフィルタリングのためにラベルとオペレーターを使用します。
---

{{< docs/hero-simple key="hero" >}}

---

## Overview

他のログシステムとは異なり、Lokiはログのラベルに関するメタデータのみをインデックス化するという考えに基づいて構築されています（Prometheusのラベルと同様です）。
ログデータ自体は圧縮され、Amazon Simple Storage Service (S3) や Google Cloud Storage (GCS) などのオブジェクトストア、またはローカルファイルシステムにチャンクとして保存されます。  

## Explore

{{< card-grid key="cards" type="simple" >}}

