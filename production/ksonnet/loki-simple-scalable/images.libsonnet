{
  _images+:: {
    loki: 'grafana/loki:2.4.2',

    read: self.loki,
    write: self.loki,
  },
}
