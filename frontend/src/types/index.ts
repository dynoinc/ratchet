export interface Channel {
  id: string;
  name: string;
}

export interface Report {
  id: string;
  channelId: string;
  content: string;
  createdAt: string;
}

export interface InstantReport {
  channelId: string;
  channelName: string;
  weekRange: string;
  createdAt: string;
  incidents: Array<object>;
  topAlerts: Array<object>;
  mitigationTime: string;
}
