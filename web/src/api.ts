import axios from 'axios';

export type Field = {
  name: string;
  type: string;
  required: boolean;
  description: string;
  in?: string;
  children?: Field[];
};

export type Endpoint = {
  id: string;
  tag: string;
  path: string;
  method: string;
  summary: string;
  description: string;
  operation_id: string;
  request: Field[];
  response: Field[];
};

export type APIDocument = {
  info: {
    title: string;
    version: string;
    description: string;
  };
  servers: string[];
  tags: { name: string; description: string }[];
  endpoints: Endpoint[];
};

export type GenerateRequest = {
  doc: APIDocument;
  meta: {
    title: string;
    author: string;
    version: string;
  };
  endpoint_ids: string[];
};

export async function parseByUrl(url: string) {
  const res = await axios.post('/api/parse', { url });
  return res.data.doc as APIDocument;
}

export async function parseByFile(file: File) {
  const form = new FormData();
  form.append('file', file);
  const res = await axios.post('/api/parse', form, {
    headers: { 'Content-Type': 'multipart/form-data' }
  });
  return res.data.doc as APIDocument;
}

export async function generateDocx(payload: GenerateRequest) {
  const res = await axios.post('/api/generate', payload, {
    responseType: 'blob'
  });
  return res.data as Blob;
}
