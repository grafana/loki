{
  _images+:: {
    loki: 'grafana/loki:2.9.1',

    read: self.loki,
    write: self.loki,
  },
}
