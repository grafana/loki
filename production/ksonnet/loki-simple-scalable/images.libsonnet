{
  _images+:: {
    loki: 'grafana/loki:2.9.2',

    read: self.loki,
    write: self.loki,
  },
}
