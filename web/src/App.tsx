import { useEffect, useMemo, useState } from 'react';
import {
  Button,
  Card,
  Col,
  Collapse,
  Divider,
  Form,
  Input,
  Layout,
  message,
  Row,
  Space,
  Tag,
  Tree,
  Typography,
  Upload
} from 'antd';
import type { DataNode } from 'antd/es/tree';
import { InboxOutlined } from '@ant-design/icons';
import {
  APIDocument,
  Endpoint,
  Field,
  generateDocx,
  parseByFile,
  parseByUrl
} from './api';

const { Header, Content } = Layout;
const { Title, Text } = Typography;

function buildTree(doc: APIDocument): DataNode[] {
  return buildTreeFromEndpoints(doc.endpoints);
}

function buildTreeFromEndpoints(endpoints: Endpoint[]): DataNode[] {
  const groups = new Map<string, Endpoint[]>();
  for (const ep of endpoints) {
    const tag = ep.tag || '未分组';
    if (!groups.has(tag)) groups.set(tag, []);
    groups.get(tag)!.push(ep);
  }
  return Array.from(groups.entries()).map(([tag, eps]) => ({
    title: tag,
    key: `tag:${tag}`,
    children: eps.map((e) => ({
      title: `${e.summary || e.operation_id || e.path} ${e.path}`,
      key: e.id,
      isLeaf: true
    }))
  }));
}

function FieldTable({ fields, isRequest }: { fields: Field[]; isRequest?: boolean }) {
  return (
    <div className="field-table">
      {isRequest ? (
        <>
          <div className="field-row field-head field-req-row">
            <div>字段名称</div>
            <div>字段类型</div>
            <div>参数位置</div>
            <div>是否必传</div>
            <div>备注</div>
          </div>
          {fields.map((f) => (
            <div key={f.name} className="field-row field-req-row">
              <div style={{ fontWeight: 500 }}>{f.name}</div>
              <div>{f.type}</div>
              <div style={{ color: '#2563eb', fontWeight: 600 }}>{f.in || 'body'}</div>
              <div>{f.required ? '是' : '否'}</div>
              <div>{f.description}</div>
            </div>
          ))}
        </>
      ) : (
        <>
          <div className="field-row field-head field-resp-row">
            <div>字段名称</div>
            <div>字段类型</div>
            <div>备注</div>
          </div>
          {fields.map((f) => (
            <div key={f.name} className="field-row field-resp-row">
              <div style={{ fontWeight: 500 }}>{f.name}</div>
              <div>{f.type}</div>
              <div>{f.description}</div>
            </div>
          ))}
        </>
      )}
    </div>
  );
}

function EndpointPreview({ endpoint }: { endpoint?: Endpoint }) {
  if (!endpoint) {
    return (
      <Card className="preview-card">
        <Text type="secondary">选择左侧接口查看详情</Text>
      </Card>
    );
  }

  return (
    <Card className="preview-card" title={`${endpoint.method} ${endpoint.path}`}>
      <Space direction="vertical" size={12} style={{ width: '100%' }}>
        <div>
          <Text strong>接口名称：</Text>
          <Text>{endpoint.summary || endpoint.operation_id || endpoint.path}</Text>
        </div>
        <div>
          <Text strong>接口说明：</Text>
          <Text>{endpoint.description || '无'}</Text>
        </div>
        <Divider />
        <Collapse
          defaultActiveKey={['req', 'resp']}
          items={[
            {
              key: 'req',
              label: '请求参数',
              children: <FieldTable fields={endpoint.request} isRequest={true} />
            },
            {
              key: 'resp',
              label: '响应参数',
              children: <FieldTable fields={endpoint.response} isRequest={false} />
            }
          ]}
        />
      </Space>
    </Card>
  );
}

export default function App() {
  const [form] = Form.useForm();
  const [doc, setDoc] = useState<APIDocument | null>(null);
  const [selectedKeys, setSelectedKeys] = useState<string[]>([]);
  const [activeEndpoint, setActiveEndpoint] = useState<Endpoint | undefined>(undefined);
  const [loading, setLoading] = useState(false);
  const [search, setSearch] = useState('');

  const endpoints = doc?.endpoints ?? [];
  const filteredEndpoints = useMemo(() => {
    if (!doc) return [];
    const keyword = search.trim().toLowerCase();
    if (!keyword) return doc.endpoints;
    return doc.endpoints.filter((e) => {
      const hay = [e.path, e.method, e.summary, e.operation_id, e.tag]
        .filter(Boolean)
        .join(' ')
        .toLowerCase();
      return hay.includes(keyword);
    });
  }, [doc, search]);

  const treeData = useMemo(() => (doc ? buildTreeFromEndpoints(filteredEndpoints) : []), [doc, filteredEndpoints]);
  const allEndpointKeys = useMemo(() => endpoints.map((e) => e.id), [endpoints]);
  const selectedCount = selectedKeys.filter((k) => endpoints.some((e) => e.id === k)).length;

  const onSelect = (keys: React.Key[]) => {
    const key = keys[0] as string | undefined;
    if (!key) return;
    const ep = endpoints.find((e) => e.id === key);
    if (ep) setActiveEndpoint(ep);
  };

  const onCheck = (keys: React.Key[] | { checked: React.Key[] }) => {
    const checked = (Array.isArray(keys) ? keys : keys.checked) as string[];
    setSelectedKeys((prev) => {
      // 1. 保留 prev 中依然在 checked 中的 key（维持原有顺序）
      const retained = prev.filter((k) => checked.includes(k));
      // 2. 找出 checked 中新增的 key，并追加到末尾
      const prevSet = new Set(prev);
      const added = checked.filter((k) => !prevSet.has(k));
      return [...retained, ...added];
    });
  };

  const checkAll = () => setSelectedKeys(allEndpointKeys);
  const clearAll = () => setSelectedKeys([]);

  const handleParseUrl = async (values: { url: string }) => {
    try {
      setLoading(true);
      const parsed = await parseByUrl(values.url);
      setDoc(parsed);
      setSelectedKeys([]);
      setActiveEndpoint(undefined);
      localStorage.setItem('lastSwaggerUrl', values.url);
      message.success('解析成功');
    } catch (err) {
      message.error('解析失败');
    } finally {
      setLoading(false);
    }
  };

  const handleUpload = async (file: File) => {
    try {
      setLoading(true);
      const parsed = await parseByFile(file);
      setDoc(parsed);
      setSelectedKeys([]);
      setActiveEndpoint(undefined);
      message.success('解析成功');
    } catch (err) {
      message.error('解析失败');
    } finally {
      setLoading(false);
    }
    return false;
  };

  useEffect(() => {
    const last = localStorage.getItem('lastSwaggerUrl');
    if (last) {
      form.setFieldsValue({ url: last });
    }
  }, [form]);

  const handleGenerate = async (meta: { title: string; author: string; version: string }) => {
    if (!doc) return;
    try {
      setLoading(true);
      const blob = await generateDocx({
        doc,
        meta,
        endpoint_ids: selectedKeys.filter((k) => endpoints.some((e) => e.id === k))
      });
      const url = window.URL.createObjectURL(blob);
      const a = document.createElement('a');
      a.href = url;
      a.download = `${meta.title || 'api'}.docx`;
      a.click();
      window.URL.revokeObjectURL(url);
    } catch (err) {
      message.error('生成失败');
    } finally {
      setLoading(false);
    }
  };

  const tagGroups = useMemo(() => {
    const groups = new Map<string, string[]>();
    for (const ep of filteredEndpoints) {
      const tag = ep.tag || '未分组';
      if (!groups.has(tag)) groups.set(tag, []);
      groups.get(tag)!.push(ep.id);
    }
    return Array.from(groups.entries());
  }, [filteredEndpoints]);

  const toggleTag = (tag: string) => {
    const ids = tagGroups.find(([t]) => t === tag)?.[1] ?? [];
    const allSelected = ids.every((id) => selectedKeys.includes(id));
    if (allSelected) {
      setSelectedKeys((prev) => prev.filter((k) => !ids.includes(k)));
    } else {
      setSelectedKeys((prev) => [...prev, ...ids.filter((id) => !prev.includes(id))]);
    }
  };

  return (
    <Layout className="app">
      <Header className="app-header">
        <Title
          level={3}
          style={{ margin: 0, color: '#0f172a', cursor: 'pointer' }}
          onClick={() => {
            setDoc(null);
            setSelectedKeys([]);
            setActiveEndpoint(undefined);
          }}
        >
          OpenAPI2Word
        </Title>
        <Text type="secondary">Swagger/OpenAPI 文档转 Word</Text>
      </Header>
      <Content className="app-content">
        {!doc ? (
          <Row gutter={[16, 16]} className="import-row">
            <Col xs={24}>
              <Card title="导入 Swagger JSON 地址" className="import-card">
                <Form form={form} layout="vertical" onFinish={handleParseUrl}>
                  <Form.Item name="url" label="Swagger JSON URL" rules={[{ required: true }]}> 
                    <Input placeholder="https://example.com/openapi.json" />
                  </Form.Item>
                  <Button type="primary" htmlType="submit" loading={loading}>解析</Button>
                </Form>
              </Card>
            </Col>
            <Col xs={24}>
              <Card title="上传 JSON/YAML 文件" className="import-card">
                <Upload.Dragger multiple={false} beforeUpload={handleUpload} showUploadList={false}>
                  <p className="ant-upload-drag-icon">
                    <InboxOutlined />
                  </p>
                  <p className="ant-upload-text">拖拽文件到此处，或点击上传</p>
                </Upload.Dragger>
              </Card>
            </Col>
          </Row>
        ) : (
          <Row gutter={16} className="preview-row">
            <Col xs={24} md={7}>
              <Card title="接口目录">
                <Space className="tree-actions" wrap>
                  <Button size="small" onClick={checkAll}>全选</Button>
                  <Button size="small" onClick={clearAll}>清空</Button>
                </Space>
                <Input.Search
                  placeholder="搜索接口"
                  allowClear
                  value={search}
                  onChange={(e) => setSearch(e.target.value)}
                  style={{ marginBottom: 12 }}
                />
                <Collapse
                  items={[
                    {
                      key: 'tags',
                      label: '按 Tag 选择',
                      children: (
                        <div className="tag-list">
                          {tagGroups.map(([tag]) => {
                            const ids = tagGroups.find(([t]) => t === tag)?.[1] ?? [];
                            const allSelected = ids.every((id) => selectedKeys.includes(id));
                            return (
                              <Tag
                                key={tag}
                                color={allSelected ? 'blue' : 'default'}
                                className="tag-chip"
                                onClick={() => toggleTag(tag)}
                              >
                                {tag}
                              </Tag>
                            );
                          })}
                        </div>
                      )
                    }
                  ]}
                  style={{ marginBottom: 12 }}
                />
                <Tree
                  checkable
                  onSelect={onSelect}
                  onCheck={onCheck}
                  checkedKeys={selectedKeys}
                  treeData={treeData}
                  defaultExpandAll
                />
              </Card>
            </Col>
            <Col xs={24} md={17}>
              <EndpointPreview endpoint={activeEndpoint} />
              <Divider />
              <Card title="文档配置">
                <Form
                  layout="vertical"
                  initialValues={{
                    title: doc.info.title || 'API 文档',
                    author: '',
                    version: doc.info.version || '1.0'
                  }}
                  onFinish={handleGenerate}
                >
                  <Row gutter={12}>
                    <Col xs={24} md={8}>
                      <Form.Item name="title" label="标题" rules={[{ required: true }]}> 
                        <Input />
                      </Form.Item>
                    </Col>
                    <Col xs={24} md={8}>
                      <Form.Item name="author" label="作者">
                        <Input />
                      </Form.Item>
                    </Col>
                    <Col xs={24} md={8}>
                      <Form.Item name="version" label="版本号">
                        <Input />
                      </Form.Item>
                    </Col>
                  </Row>
                  <Button type="primary" htmlType="submit" loading={loading} disabled={selectedCount === 0}>
                    开始生成
                  </Button>
                </Form>
              </Card>
            </Col>
          </Row>
        )}
      </Content>

      {doc && (
        <div className="floating-bar">
          <Space>
            <Tag color="#2563eb">已选 {selectedCount} 个接口</Tag>
            <Button type="primary" onClick={() => {
              const form = document.querySelector('form');
              if (form) (form as HTMLFormElement).requestSubmit();
            }} disabled={selectedCount === 0}>
              开始生成
            </Button>
          </Space>
        </div>
      )}
    </Layout>
  );
}
