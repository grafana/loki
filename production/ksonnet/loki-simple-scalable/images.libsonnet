{
  _images+:: {
    loki: 'grafana/loki:2.9.4',

    read: self.loki,
    write: self.loki,
  },
}
