import { Checkbox, Space, Typography } from "antd";
import { useState } from "react";
import type { Share } from "../api/types";

export function ShareCreatedContent({ share }: { share: Share }) {
  const [includeCode, setIncludeCode] = useState(false);
  const url = new URL(share.url!, window.location.origin);
  if (includeCode && share.code) url.searchParams.set("code", share.code);

  return (
    <Space direction="vertical" size={16} className="full-width">
      <Typography.Text type="secondary">链接和提取码可随时在“我的分享”中查看。勾选携带密码后，接收者打开链接即可自动验证。</Typography.Text>
      <Checkbox checked={includeCode} disabled={!share.code} onChange={(event) => setIncludeCode(event.target.checked)}>携带密码</Checkbox>
      <Typography.Text copyable code>{url.toString()}</Typography.Text>
      {share.code && <div className="share-created-code">
        <Typography.Text type="secondary">4 位数字提取码</Typography.Text>
        <Typography.Title level={3} copyable={{ text: share.code ?? "" }}>{share.code}</Typography.Title>
      </div>}
    </Space>
  );
}
